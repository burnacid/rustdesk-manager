package db

import "time"

// Indexed strings use size:191 so the schema works on MySQL/MariaDB utf8mb4.
// JSON blobs are stored as text for portability between SQLite and MySQL.

// User status values match the RustDesk client: 1 normal, 0 disabled, -1 unverified.
const (
	UserDisabled   = 0
	UserNormal     = 1
	UserUnverified = -1
)

type User struct {
	ID           uint   `gorm:"primaryKey"`
	Name         string `gorm:"size:191;uniqueIndex;not null"`
	DisplayName  string `gorm:"size:191"`
	Email        string `gorm:"size:191;index"`
	PasswordHash string `gorm:"size:255"`
	IsAdmin      bool
	Status       int    `gorm:"default:1"`
	Note         string `gorm:"type:text"`
	TOTPSecret   string `gorm:"type:text"` // encrypted; set during setup, active once TOTPEnabled
	TOTPEnabled  bool
	TOTPLastStep int64 // last accepted time step, so a code can't be used twice
	UserGroupID  *uint
	UserGroup    *UserGroup
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type UserGroup struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:191;uniqueIndex;not null"`
	Note      string `gorm:"type:text"`
	CreatedAt time.Time
}

// UserGroupAccess lets members of a user group see the devices in a device group.
type UserGroupAccess struct {
	UserGroupID   uint `gorm:"primaryKey"`
	DeviceGroupID uint `gorm:"primaryKey"`
}

// Token is a client login (RustDesk app) or admin UI session. Only the hash is stored.
type Token struct {
	ID         uint   `gorm:"primaryKey"`
	Hash       string `gorm:"size:64;uniqueIndex;not null"`
	Kind       string `gorm:"size:16;index"` // "client", "web" or "api"
	Name       string `gorm:"size:191"`      // label of an API token
	UserID     uint   `gorm:"index;not null"`
	User       User
	DeviceID   string `gorm:"size:191"` // RustDesk ID of the controlling client
	DeviceUUID string `gorm:"size:191"`
	DeviceInfo string `gorm:"type:text"` // {"os","type","name"}
	LastUsedAt time.Time
	ExpiresAt  *time.Time
	CreatedAt  time.Time
}

type Device struct {
	ID         uint   `gorm:"primaryKey"`
	RustdeskID string `gorm:"size:191;uniqueIndex;not null"`
	UUID       string `gorm:"size:191"` // bound on first sysinfo; mismatches are rejected
	PK         string `gorm:"size:191"` // public key, from /api/devices/deploy
	Hostname   string `gorm:"size:191"`
	OS         string `gorm:"size:255"`
	CPU        string `gorm:"size:255"`
	Memory     string `gorm:"size:64"`
	Username   string `gorm:"size:191"`
	Version    string `gorm:"size:64"`
	// Names set by an admin or `rustdesk --assign`; they win over what the device reports.
	CustomHostname string `gorm:"size:191"`
	CustomUsername string `gorm:"size:191"`
	Sysinfo        string `gorm:"type:text"` // raw sysinfo JSON as sent by the client
	SysinfoHash    string `gorm:"size:64"`
	Note           string `gorm:"type:text"`
	UserID         *uint
	User           *User
	DeviceGroupID  *uint
	DeviceGroup    *DeviceGroup
	StrategyID     *uint
	Strategy       *Strategy
	// StrategyKeys lists option keys this server has pushed to the device, so keys
	// removed from its strategy can be reset ("" = client default). JSON []string.
	StrategyKeys string `gorm:"type:text"`
	LastSeenAt   *time.Time
	LastIP       string `gorm:"size:64"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// DisplayHostname is the admin-set name if any, else what the device reports.
func (d *Device) DisplayHostname() string {
	if d.CustomHostname != "" {
		return d.CustomHostname
	}
	return d.Hostname
}

// DisplayUsername is the admin-set username if any, else the signed-in user on the device.
func (d *Device) DisplayUsername() string {
	if d.CustomUsername != "" {
		return d.CustomUsername
	}
	return d.Username
}

type DeviceGroup struct {
	ID         uint   `gorm:"primaryKey"`
	Name       string `gorm:"size:191;uniqueIndex;not null"`
	Note       string `gorm:"type:text"`
	StrategyID *uint
	Strategy   *Strategy
	CreatedAt  time.Time
}

// Strategy is a set of client options pushed through the heartbeat response.
type Strategy struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:191;uniqueIndex;not null"`
	Options   string `gorm:"type:text"` // JSON map[string]string
	IsDefault bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Address book share rules, as used by the client.
const (
	RuleRead        = 1
	RuleReadWrite   = 2
	RuleFullControl = 3
)

type AddressBook struct {
	ID        uint   `gorm:"primaryKey"`
	GUID      string `gorm:"size:64;uniqueIndex;not null"`
	Name      string `gorm:"size:191;not null"`
	OwnerID   uint   `gorm:"index"`
	Owner     User
	Personal  bool   `gorm:"index"`
	Note      string `gorm:"type:text"`
	Info      string `gorm:"type:text"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AbShare gives a user or user group access to a shared address book.
type AbShare struct {
	ID            uint  `gorm:"primaryKey"`
	AddressBookID uint  `gorm:"index;not null"`
	UserID        *uint `gorm:"index"`
	UserGroupID   *uint `gorm:"index"`
	Rule          int
}

type AbPeer struct {
	ID               uint   `gorm:"primaryKey"`
	AddressBookID    uint   `gorm:"uniqueIndex:idx_ab_peer;not null"`
	PeerID           string `gorm:"size:191;uniqueIndex:idx_ab_peer;not null"`
	Alias            string `gorm:"size:191"`
	Tags             string `gorm:"type:text"` // JSON []string
	Hash             string `gorm:"type:text"` // personal books: client password hash, encrypted
	Password         string `gorm:"type:text"` // shared books: device password, encrypted
	Username         string `gorm:"size:191"`
	Hostname         string `gorm:"size:191"`
	Platform         string `gorm:"size:64"`
	Note             string `gorm:"type:text"`
	ForceAlwaysRelay bool
	RdpPort          string `gorm:"size:16"`
	RdpUsername      string `gorm:"size:191"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AbTag struct {
	ID            uint   `gorm:"primaryKey"`
	AddressBookID uint   `gorm:"uniqueIndex:idx_ab_tag;not null"`
	Name          string `gorm:"size:191;uniqueIndex:idx_ab_tag;not null"`
	Color         int64  // ARGB as used by Flutter
}

// LegacyAb stores the single-blob address book used by GET/POST /api/ab.
type LegacyAb struct {
	UserID    uint   `gorm:"primaryKey"`
	Data      string `gorm:"type:longtext"`
	UpdatedAt time.Time
}

type AuditConn struct {
	ID          uint      `gorm:"primaryKey"`
	GUID        string    `gorm:"size:64;uniqueIndex"`
	Nonce       *string   `gorm:"size:64;uniqueIndex"`         // nil for old clients that send none
	DeviceID    string    `gorm:"size:191;index:idx_conn_dev"` // controlled device
	DeviceUUID  string    `gorm:"size:191"`
	ConnID      int64     `gorm:"index:idx_conn_dev"`
	SessionID   string    `gorm:"size:32"` // u64 in the client, kept as text
	IP          string    `gorm:"size:64"`
	PeerID      string    `gorm:"size:191;index"` // controlling side
	PeerName    string    `gorm:"size:191"`
	Type        int       // 0 remote, 1 file transfer, 2 port forward, 3 camera, 4 terminal
	PrimaryAuth int       // 1 click, 2 temporary password, 3 permanent password, 4 switch sides
	TwoFactor   int       // 1 TOTP, 2 trusted device
	Note        string    `gorm:"type:text"`
	StartedAt   time.Time `gorm:"index"`
	AuthedAt    *time.Time
	ClosedAt    *time.Time
}

type AuditFile struct {
	ID        uint    `gorm:"primaryKey"`
	Nonce     *string `gorm:"size:64;uniqueIndex"`
	DeviceID  string  `gorm:"size:191;index"`
	PeerID    string  `gorm:"size:191;index"`
	ConnID    int64
	Type      int    // 0 remote send (to controller), 1 remote receive
	Path      string `gorm:"type:text"`
	IsFile    bool
	Info      string    `gorm:"type:text"` // JSON: ip, name, num, files
	CreatedAt time.Time `gorm:"index"`
}

type AuditAlarm struct {
	ID        uint    `gorm:"primaryKey"`
	Nonce     *string `gorm:"size:64;uniqueIndex"`
	DeviceID  string  `gorm:"size:191;index"`
	ConnID    int64
	Type      int       // AlarmAuditType from connection.rs
	Info      string    `gorm:"type:text"`
	CreatedAt time.Time `gorm:"index"`
}

type Recording struct {
	ID        uint   `gorm:"primaryKey"`
	File      string `gorm:"size:191;uniqueIndex;not null"`
	DeviceID  string `gorm:"size:191;index"`
	Size      int64
	Completed bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type OIDCProvider struct {
	ID           uint   `gorm:"primaryKey"`
	Name         string `gorm:"size:64;uniqueIndex;not null"` // button in the client; known names get an icon
	Issuer       string `gorm:"size:255"`
	ClientID     string `gorm:"size:255"`
	ClientSecret string `gorm:"type:text"` // encrypted
	Scopes       string `gorm:"size:255"`  // extra scopes besides openid, email, profile
	AutoCreate   bool   // create a user on first login
	// TrustEmail matches existing users by email even when the provider doesn't
	// mark it verified (e.g. Microsoft Entra). Only for providers you control.
	TrustEmail bool
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// OIDCIdentity links a provider account (issuer subject) to a user.
type OIDCIdentity struct {
	ID         uint   `gorm:"primaryKey"`
	ProviderID uint   `gorm:"uniqueIndex:idx_oidc_sub;not null"`
	Subject    string `gorm:"size:191;uniqueIndex:idx_oidc_sub;not null"`
	UserID     uint   `gorm:"index;not null"`
	Email      string `gorm:"size:191"`
	CreatedAt  time.Time
}

// OIDCSession tracks one SSO login between start and callback (and, for the
// RustDesk app, until it collects the result with auth-query).
type OIDCSession struct {
	Code       string `gorm:"size:64;primaryKey"` // handed to the client; also the start URL key
	State      string `gorm:"size:64;uniqueIndex"`
	Kind       string `gorm:"size:16"` // "client" or "web"
	ProviderID uint
	DeviceID   string    `gorm:"size:191"`
	DeviceUUID string    `gorm:"size:191"`
	DeviceInfo string    `gorm:"type:text"`
	Verifier   string    `gorm:"size:191"` // PKCE
	Nonce      string    `gorm:"size:64"`
	Result     string    `gorm:"type:text"` // login response JSON once finished (contains the token)
	Error      string    `gorm:"type:text"`
	ExpiresAt  time.Time `gorm:"index"`
}

// DeployToken authorises `rustdesk --assign` and `--deploy` from install scripts.
type DeployToken struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:191"`
	Hash      string `gorm:"size:64;uniqueIndex;not null"`
	Prefix    string `gorm:"size:16"` // first characters, shown in the UI
	ExpiresAt *time.Time
	CreatedAt time.Time
}

// Passkey is a WebAuthn credential for signing in to the web console.
type Passkey struct {
	ID           uint   `gorm:"primaryKey"`
	UserID       uint   `gorm:"index;not null"`
	Name         string `gorm:"size:191"`
	CredentialID string `gorm:"size:191;uniqueIndex;not null"` // base64url
	Data         string `gorm:"type:text"`                     // webauthn.Credential as JSON
	LastUsedAt   *time.Time
	CreatedAt    time.Time
}

// AdminEvent records one change made through the admin API (never the request body).
type AdminEvent struct {
	ID        uint   `gorm:"primaryKey"`
	UserID    uint   `gorm:"index"`
	UserName  string `gorm:"size:191"`
	Method    string `gorm:"size:8"`
	Route     string `gorm:"size:191"` // chi route pattern, e.g. /devices/{id}
	TargetID  string `gorm:"size:191"`
	Status    int
	IP        string    `gorm:"size:64"`
	Via       string    `gorm:"size:8"` // "web" or "api"
	CreatedAt time.Time `gorm:"index"`
}

// Setting is a simple key/value store for runtime options edited in the UI.
type Setting struct {
	Key   string `gorm:"size:191;primaryKey"`
	Value string `gorm:"type:text"`
}

// Explicit names: GORM would otherwise call these "o_id_c_...".
func (OIDCProvider) TableName() string { return "oidc_providers" }
func (OIDCIdentity) TableName() string { return "oidc_identities" }
func (OIDCSession) TableName() string  { return "oidc_sessions" }

func allModels() []any {
	return []any{
		&UserGroup{}, &UserGroupAccess{}, &User{}, &Token{}, &Strategy{}, &DeviceGroup{}, &Device{},
		&AddressBook{}, &AbShare{}, &AbPeer{}, &AbTag{}, &LegacyAb{},
		&AuditConn{}, &AuditFile{}, &AuditAlarm{}, &Recording{},
		&OIDCProvider{}, &OIDCIdentity{}, &OIDCSession{}, &DeployToken{}, &Setting{},
		&Passkey{}, &AdminEvent{},
	}
}
