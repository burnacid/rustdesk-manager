// Package abook implements address books: one personal book per user plus shared
// books created by admins and shared with users or user groups.
//
// Peer and tag JSON follow the client (reference/rustdesk/flutter/lib/models/
// ab_model.dart and peer_model.dart).
package abook

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/secret"
)

// UserError is a message meant for the person using the client; other errors
// (database failures) are logged and reported as a generic server error.
type UserError string

func (e UserError) Error() string { return string(e) }

const (
	ErrNotFound  = UserError("Address book not found")
	ErrReadOnly  = UserError("You only have read access to this address book")
	ErrDuplicate = UserError("This ID is already in the address book")
	ErrFull      = UserError("exceed_max_devices") // translated by the client
)

// MaxPeers per address book; 0 = unlimited.
const MaxPeers = 0

type Service struct {
	db  *gorm.DB
	box *secret.Box
}

func New(gdb *gorm.DB, box *secret.Box) *Service { return &Service{db: gdb, box: box} }

const PersonalName = "My address book"

// Personal returns the user's personal book, creating it on first use.
func (s *Service) Personal(u *db.User) (*db.AddressBook, error) {
	var ab db.AddressBook
	err := s.db.Where("owner_id = ? AND personal = ?", u.ID, true).Limit(1).Find(&ab).Error
	if err != nil || ab.ID != 0 {
		return &ab, err
	}
	ab = db.AddressBook{GUID: uuid.NewString(), Name: PersonalName, OwnerID: u.ID, Personal: true}
	return &ab, s.db.Create(&ab).Error
}

// Rule returns the user's access to a book (0 = none). Owners have full control;
// shares to the user or the user's group grant their rule (the highest wins).
func (s *Service) Rule(u *db.User, ab *db.AddressBook) int {
	if ab.OwnerID == u.ID {
		return db.RuleFullControl
	}
	if ab.Personal {
		return 0
	}
	q := s.db.Model(&db.AbShare{}).Where("address_book_id = ?", ab.ID)
	if u.UserGroupID != nil {
		q = q.Where("user_id = ? OR user_group_id = ?", u.ID, *u.UserGroupID)
	} else {
		q = q.Where("user_id = ?", u.ID)
	}
	var rule int
	if err := q.Select("COALESCE(MAX(rule), 0)").Row().Scan(&rule); err != nil {
		return 0
	}
	return rule
}

// Open loads a book by guid and checks access. write=true requires read/write.
func (s *Service) Open(u *db.User, guid string, write bool) (*db.AddressBook, int, error) {
	var ab db.AddressBook
	if err := s.db.Where("guid = ?", guid).Limit(1).Find(&ab).Error; err != nil {
		return nil, 0, err
	}
	if ab.ID == 0 {
		return nil, 0, ErrNotFound
	}
	rule := s.Rule(u, &ab)
	if rule == 0 {
		return nil, 0, ErrNotFound
	}
	if write && rule < db.RuleReadWrite {
		return nil, 0, ErrReadOnly
	}
	return &ab, rule, nil
}

type Profile struct {
	GUID  string `json:"guid"`
	Name  string `json:"name"`
	Owner string `json:"owner"`
	Note  string `json:"note"`
	Rule  int    `json:"rule"`
}

// SharedProfiles lists the shared books a user can open (not their personal one).
func (s *Service) SharedProfiles(u *db.User) ([]Profile, error) {
	ids := s.db.Model(&db.AbShare{}).Select("address_book_id").Where("user_id = ?", u.ID)
	if u.UserGroupID != nil {
		ids = s.db.Model(&db.AbShare{}).Select("address_book_id").
			Where("user_id = ? OR user_group_id = ?", u.ID, *u.UserGroupID)
	}
	var books []db.AddressBook
	err := s.db.Preload("Owner").Where("personal = ?", false).
		Where(s.db.Where("owner_id = ?", u.ID).Or("id IN (?)", ids)).
		Order("name").Find(&books).Error
	if err != nil {
		return nil, err
	}
	out := make([]Profile, 0, len(books))
	for i := range books {
		b := &books[i]
		out = append(out, Profile{GUID: b.GUID, Name: b.Name, Owner: b.Owner.Name, Note: b.Note, Rule: s.Rule(u, b)})
	}
	return out, nil
}

// Peer is the client's address book peer JSON.
type Peer struct {
	ID               string   `json:"id"`
	Hash             string   `json:"hash,omitempty"`
	Password         string   `json:"password,omitempty"`
	Username         string   `json:"username"`
	Hostname         string   `json:"hostname"`
	Platform         string   `json:"platform"`
	Alias            string   `json:"alias"`
	Tags             []string `json:"tags"`
	ForceAlwaysRelay string   `json:"forceAlwaysRelay"` // "true"/"false" in the client
	RdpPort          string   `json:"rdpPort"`
	RdpUsername      string   `json:"rdpUsername"`
	Note             string   `json:"note"`
	LoginName        string   `json:"loginName"`
	DeviceGroupName  string   `json:"device_group_name"`
	// SameServer tells the client this server knows the device, so it shows our
	// username/hostname/platform instead of syncing them from its recent list.
	SameServer bool `json:"same_server"`
	// HasPassword is for the web console, which never sees the secret itself.
	HasPassword bool `json:"-"`
}

// Platform maps the client's sysinfo "os" ("windows / Windows 11 ...") to its platform names.
func Platform(os string) string {
	first := strings.ToLower(strings.TrimSpace(strings.SplitN(os, " / ", 2)[0]))
	switch {
	case first == "windows":
		return "Windows"
	case first == "macos":
		return "Mac OS"
	case first == "android":
		return "Android"
	case first == "linux", strings.Contains(strings.ToLower(os), "linux"):
		return "Linux"
	}
	return ""
}

// Peers returns a page of a book's peers, merged with what this server knows about each device.
// withSecrets=false hides password/hash (web console).
func (s *Service) Peers(ab *db.AddressBook, offset, limit int, withSecrets bool) ([]Peer, int64, error) {
	var total int64
	tx := s.db.Model(&db.AbPeer{}).Where("address_book_id = ?", ab.ID)
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []db.AbPeer
	if err := tx.Order("peer_id").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.PeerID)
	}
	devices := map[string]db.Device{}
	if len(ids) > 0 {
		var ds []db.Device
		s.db.Preload("User").Preload("DeviceGroup").Where("rustdesk_id IN ?", ids).Find(&ds)
		for _, d := range ds {
			devices[d.RustdeskID] = d
		}
	}
	out := make([]Peer, 0, len(rows))
	for _, r := range rows {
		p := Peer{ID: r.PeerID, Username: r.Username, Hostname: r.Hostname, Platform: r.Platform,
			Alias: r.Alias, Tags: decodeTags(r.Tags), ForceAlwaysRelay: fmt.Sprint(r.ForceAlwaysRelay),
			RdpPort: r.RdpPort, RdpUsername: r.RdpUsername, Note: r.Note}
		p.HasPassword = r.Password != "" || r.Hash != ""
		if withSecrets {
			p.Hash, _ = s.box.Open(r.Hash)
			p.Password, _ = s.box.Open(r.Password)
		}
		if d, ok := devices[r.PeerID]; ok {
			p.SameServer = true
			if u := d.DisplayUsername(); u != "" {
				p.Username = u
			}
			if h := d.DisplayHostname(); h != "" {
				p.Hostname = h
			}
			if pl := Platform(d.OS); pl != "" {
				p.Platform = pl
			}
			if d.User != nil {
				p.LoginName = d.User.Name
			}
			if d.DeviceGroup != nil {
				p.DeviceGroupName = d.DeviceGroup.Name
			}
		}
		out = append(out, p)
	}
	return out, total, nil
}

func decodeTags(s string) []string {
	var t []string
	_ = json.Unmarshal([]byte(s), &t)
	if t == nil {
		t = []string{}
	}
	return t
}

func encodeTags(t []string) string {
	b, _ := json.Marshal(t)
	return string(b)
}

// PeerFields is a partial peer update. Absent fields (nil) are left unchanged, as
// the client sends only what changed ("Use Map rather than Peer to distinguish
// between empty and null").
type PeerFields struct {
	ID               string    `json:"id"`
	Alias            *string   `json:"alias"`
	Tags             *[]string `json:"tags"`
	Hash             *string   `json:"hash"`
	Password         *string   `json:"password"`
	Username         *string   `json:"username"`
	Hostname         *string   `json:"hostname"`
	Platform         *string   `json:"platform"`
	Note             *string   `json:"note"`
	ForceAlwaysRelay *string   `json:"forceAlwaysRelay"`
	RdpPort          *string   `json:"rdpPort"`
	RdpUsername      *string   `json:"rdpUsername"`
}

// existingTags keeps only tags defined in the book, without duplicates.
func (s *Service) existingTags(ab *db.AddressBook, tags []string) []string {
	var names []string
	s.db.Model(&db.AbTag{}).Where("address_book_id = ?", ab.ID).Pluck("name", &names)
	out := []string{}
	for _, t := range tags {
		if slices.Contains(names, t) && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

func (s *Service) apply(ab *db.AddressBook, p *db.AbPeer, f PeerFields) {
	set := func(dst *string, src *string, max int) {
		if src != nil {
			v := *src
			if len(v) > max {
				v = v[:max]
			}
			*dst = v
		}
	}
	set(&p.Alias, f.Alias, 191)
	set(&p.Username, f.Username, 191)
	set(&p.Hostname, f.Hostname, 191)
	set(&p.Platform, f.Platform, 64)
	set(&p.Note, f.Note, 2000)
	set(&p.RdpPort, f.RdpPort, 16)
	set(&p.RdpUsername, f.RdpUsername, 191)
	if f.Tags != nil {
		p.Tags = encodeTags(s.existingTags(ab, *f.Tags))
	}
	if f.ForceAlwaysRelay != nil {
		p.ForceAlwaysRelay = *f.ForceAlwaysRelay == "true"
	}
	// Personal books keep the client's password hash; shared books keep a password
	// that everyone with access can use. Never store the other kind.
	if ab.Personal && f.Hash != nil {
		p.Hash = s.box.Seal(*f.Hash)
	}
	if !ab.Personal && f.Password != nil {
		p.Password = s.box.Seal(*f.Password)
	}
}

func validID(id string) bool {
	return id != "" && len(id) <= 100 && !strings.ContainsAny(id, " \t\r\n")
}

// AddPeer adds a peer; it fails if the id is already in the book.
func (s *Service) AddPeer(ab *db.AddressBook, f PeerFields) error {
	f.ID = strings.TrimSpace(f.ID)
	if !validID(f.ID) {
		return UserError("Invalid ID")
	}
	var n int64
	s.db.Model(&db.AbPeer{}).Where("address_book_id = ? AND peer_id = ?", ab.ID, f.ID).Count(&n)
	if n > 0 {
		return ErrDuplicate
	}
	if MaxPeers > 0 {
		s.db.Model(&db.AbPeer{}).Where("address_book_id = ?", ab.ID).Count(&n)
		if n >= MaxPeers {
			return ErrFull
		}
	}
	p := db.AbPeer{AddressBookID: ab.ID, PeerID: f.ID, Tags: "[]"}
	s.apply(ab, &p, f)
	return s.db.Create(&p).Error
}

// UpdatePeer changes the fields present in f.
func (s *Service) UpdatePeer(ab *db.AddressBook, f PeerFields) error {
	var p db.AbPeer
	if err := s.db.Where("address_book_id = ? AND peer_id = ?", ab.ID, f.ID).Limit(1).Find(&p).Error; err != nil {
		return err
	}
	if p.ID == 0 {
		return UserError("This ID is not in the address book")
	}
	s.apply(ab, &p, f)
	return s.db.Save(&p).Error
}

func (s *Service) DeletePeers(ab *db.AddressBook, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.Where("address_book_id = ? AND peer_id IN ?", ab.ID, ids).Delete(&db.AbPeer{}).Error
}

type Tag struct {
	Name  string `json:"name"`
	Color int64  `json:"color"`
}

func (s *Service) Tags(ab *db.AddressBook) ([]Tag, error) {
	var rows []db.AbTag
	if err := s.db.Where("address_book_id = ?", ab.ID).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]Tag, 0, len(rows))
	for _, t := range rows {
		out = append(out, Tag{t.Name, t.Color})
	}
	return out, nil
}

func (s *Service) AddTag(ab *db.AddressBook, t Tag) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" || len(t.Name) > 100 {
		return UserError("Invalid tag name")
	}
	var n int64
	s.db.Model(&db.AbTag{}).Where("address_book_id = ? AND name = ?", ab.ID, t.Name).Count(&n)
	if n > 0 {
		return nil // already there; adding is idempotent
	}
	return s.db.Create(&db.AbTag{AddressBookID: ab.ID, Name: t.Name, Color: t.Color}).Error
}

func (s *Service) SetTagColor(ab *db.AddressBook, t Tag) error {
	return s.db.Model(&db.AbTag{}).Where("address_book_id = ? AND name = ?", ab.ID, t.Name).
		Update("color", t.Color).Error
}

// RenameTag renames a tag and updates every peer that uses it.
func (s *Service) RenameTag(ab *db.AddressBook, oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" || len(newName) > 100 {
		return UserError("Invalid tag name")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var n int64
		tx.Model(&db.AbTag{}).Where("address_book_id = ? AND name = ?", ab.ID, newName).Count(&n)
		if n > 0 {
			return UserError("Tag " + newName + " already exists")
		}
		if err := tx.Model(&db.AbTag{}).Where("address_book_id = ? AND name = ?", ab.ID, oldName).
			Update("name", newName).Error; err != nil {
			return err
		}
		return rewriteTags(tx, ab, func(tags []string) []string {
			for i, t := range tags {
				if t == oldName {
					tags[i] = newName
				}
			}
			return tags
		})
	})
}

// DeleteTags removes tags and takes them off every peer.
func (s *Service) DeleteTags(ab *db.AddressBook, names []string) error {
	if len(names) == 0 {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("address_book_id = ? AND name IN ?", ab.ID, names).Delete(&db.AbTag{}).Error; err != nil {
			return err
		}
		return rewriteTags(tx, ab, func(tags []string) []string {
			return slices.DeleteFunc(tags, func(t string) bool { return slices.Contains(names, t) })
		})
	})
}

func rewriteTags(tx *gorm.DB, ab *db.AddressBook, f func([]string) []string) error {
	var peers []db.AbPeer
	if err := tx.Select("id, tags").Where("address_book_id = ?", ab.ID).Find(&peers).Error; err != nil {
		return err
	}
	for _, p := range peers {
		old := decodeTags(p.Tags)
		updated := encodeTags(f(slices.Clone(old)))
		if updated != encodeTags(old) {
			if err := tx.Model(&db.AbPeer{}).Where("id = ?", p.ID).Update("tags", updated).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteBook removes a book with its peers, tags and shares.
func (s *Service) DeleteBook(ab *db.AddressBook) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		for _, m := range []any{&db.AbPeer{}, &db.AbTag{}, &db.AbShare{}} {
			if err := tx.Where("address_book_id = ?", ab.ID).Delete(m).Error; err != nil {
				return err
			}
		}
		return tx.Delete(ab).Error
	})
}

// DeleteUserBooks runs when a user is deleted: their personal book goes, shared
// books they own move to newOwner, and shares to them are removed.
func (s *Service) DeleteUserBooks(tx *gorm.DB, userID, newOwner uint) error {
	var personal []db.AddressBook
	tx.Where("owner_id = ? AND personal = ?", userID, true).Find(&personal)
	for i := range personal {
		for _, m := range []any{&db.AbPeer{}, &db.AbTag{}, &db.AbShare{}} {
			if err := tx.Where("address_book_id = ?", personal[i].ID).Delete(m).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(&personal[i]).Error; err != nil {
			return err
		}
	}
	if err := tx.Model(&db.AddressBook{}).Where("owner_id = ?", userID).Update("owner_id", newOwner).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ?", userID).Delete(&db.AbShare{}).Error
}

// AddPreset puts a device into a shared book from its install-time presets
// (preset-address-book-name/-tag/-alias/-password/-note). Only books and tags that
// already exist are used, and a device that is already in the book is left alone.
func (s *Service) AddPreset(bookName, deviceID, tag, alias, password, note string) error {
	var ab db.AddressBook
	if err := s.db.Where("name = ? AND personal = ?", bookName, false).Limit(1).Find(&ab).Error; err != nil || ab.ID == 0 {
		return err
	}
	var n int64
	s.db.Model(&db.AbPeer{}).Where("address_book_id = ? AND peer_id = ?", ab.ID, deviceID).Count(&n)
	if n > 0 {
		return nil
	}
	f := PeerFields{ID: deviceID}
	if tag != "" {
		f.Tags = &[]string{tag}
	}
	if alias != "" {
		f.Alias = &alias
	}
	if password != "" {
		f.Password = &password
	}
	if note != "" {
		f.Note = &note
	}
	return s.AddPeer(&ab, f)
}
