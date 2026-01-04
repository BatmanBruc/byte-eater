package types

type BillingStore interface {
	IsUnlimited(userID int64) (bool, error)
	GetOrResetBalance(userID int64) (int, error)
	Consume(userID int64, credits int) (remaining int, unlimited bool, err error)
}
