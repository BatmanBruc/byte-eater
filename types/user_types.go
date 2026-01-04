package types

import "time"

type User struct {
	UserID    int64
	ChatID    int64
	Username  string
	FirstName string
	LastName  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Subscription struct {
	UserID    int64
	Plan      string
	Status    string
	ExpiresAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Payment struct {
	UserID                int64
	Provider              string
	Currency              string
	TotalAmount           int64
	InvoicePayload        string
	TelegramPaymentCharge string
	ProviderPaymentCharge string
	CreatedAt             time.Time
}

type UserStore interface {
	UpsertUser(user User) error
	GetUser(userID int64) (*User, error)

	UpsertSubscription(sub Subscription) error
	GetSubscription(userID int64) (*Subscription, error)

	IsUnlimited(userID int64) (bool, error)

	RecordPayment(p Payment) (inserted bool, err error)
	ActivateOrExtendUnlimited(userID int64, duration time.Duration) (*Subscription, error)
}
