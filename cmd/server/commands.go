package main

import (
	"fmt"
	"os"

	"gorm.io/gorm"

	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/config"
	"rustdesk-manager/internal/db"
)

const usage = `Usage:
  rustdesk-manager                                 start the server
  rustdesk-manager reset-password <user> [new]     set a new password (random if omitted)
                                                   and sign the user out everywhere
`

// runCommand handles maintenance commands and returns the process exit code.
func runCommand(gdb *gorm.DB, args []string) int {
	switch args[0] {
	case "reset-password":
		if len(args) < 2 || len(args) > 3 {
			fmt.Fprint(os.Stderr, usage)
			return 2
		}
		return resetPassword(gdb, args[1], args[2:])
	case "help", "-h", "--help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}

func resetPassword(gdb *gorm.DB, name string, rest []string) int {
	var u db.User
	if err := gdb.Where("name = ?", name).Limit(1).Find(&u).Error; err != nil {
		fmt.Fprintln(os.Stderr, "database error:", err)
		return 1
	}
	if u.ID == 0 {
		var names []string
		gdb.Model(&db.User{}).Order("name").Limit(20).Pluck("name", &names)
		fmt.Fprintf(os.Stderr, "user %q not found. Existing users: %v\n", name, names)
		return 1
	}
	password := config.RandomHex(8)
	if len(rest) == 1 {
		password = rest[0]
	}
	if len(password) < 8 {
		fmt.Fprintln(os.Stderr, "password must be at least 8 characters")
		return 2
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		// Re-enable the account too, so a locked-out admin can get back in.
		if err := tx.Model(&u).Updates(map[string]any{"password_hash": hash, "status": db.UserNormal}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", u.ID).Delete(&db.Token{}).Error
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "database error:", err)
		return 1
	}
	fmt.Printf("Password for %q reset. All of its sessions were signed out.\n", u.Name)
	if len(rest) == 0 {
		fmt.Printf("New password: %s\n", password)
	}
	return 0
}
