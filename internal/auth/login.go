package auth

import (
	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/secret"
)

// VerifyUserTOTP checks a user's 2FA code and records the step so it can't be reused.
// The update is conditional, so two requests racing with the same code can't both win.
func VerifyUserTOTP(gdb *gorm.DB, box *secret.Box, u *db.User, code string) bool {
	if !u.TOTPEnabled {
		return false
	}
	sec, err := box.Open(u.TOTPSecret)
	if err != nil {
		return false
	}
	step, ok := CheckTOTP(sec, code, u.TOTPLastStep)
	if !ok {
		return false
	}
	res := gdb.Model(&db.User{}).Where("id = ? AND totp_last_step < ?", u.ID, step).Update("totp_last_step", step)
	if res.Error != nil || res.RowsAffected != 1 {
		return false
	}
	u.TOTPLastStep = step
	return true
}
