package domain

type Wallet struct {
	CustomerID uint32
	Balance    int64
	Blocked    int64
}

func (w Wallet) Available() int64 {
	return w.Balance - w.Blocked
}
