package httpapi

import (
	"time"

	"sms-gateway/internal/app/receiver"
	"sms-gateway/internal/domain"
)

type sendRequest struct {
	ClientID   string `json:"client_id"`
	Phone      string `json:"phone"`
	Text       string `json:"text"`
	Express    bool   `json:"express"`
	TTLSeconds int    `json:"ttl_seconds"`
}

func (s sendRequest) command(customerID uint32) (receiver.SendCommand, error) {
	clientID, err := uuidParam("client_id", s.ClientID)
	if err != nil {
		return receiver.SendCommand{}, err
	}
	return receiver.SendCommand{
		CustomerID: customerID,
		ClientID:   clientID,
		Phone:      s.Phone,
		Text:       s.Text,
		Express:    s.Express,
		TTL:        time.Duration(s.TTLSeconds) * time.Second,
	}, nil
}

type createCustomerRequest struct {
	Name string `json:"name"`
}

type customerResponse struct {
	ID        uint32    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

func newCustomerResponse(c domain.Customer) customerResponse {
	return customerResponse{ID: c.ID, Name: c.Name, CreatedAt: c.CreatedAt}
}

type topUpRequest struct {
	Amount int64 `json:"amount"`
}

type messageResponse struct {
	ID         string     `json:"id"`
	CustomerID uint32     `json:"customer_id"`
	ClientID   string     `json:"client_id"`
	Phone      string     `json:"phone"`
	Text       string     `json:"text"`
	Express    bool       `json:"express"`
	Status     string     `json:"status"`
	Price      int64      `json:"price"`
	Attempts   int        `json:"attempts"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	SentAt     *time.Time `json:"sent_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

func newMessageResponse(msg domain.Message, ids *IDCodec) messageResponse {
	return messageResponse{
		ID:         ids.Encode(msg.ID),
		CustomerID: msg.CustomerID,
		ClientID:   msg.ClientID.String(),
		Phone:      msg.Phone,
		Text:       msg.Body,
		Express:    msg.Lane == domain.LaneExpress,
		Status:     string(msg.Status),
		Price:      msg.Price,
		Attempts:   msg.Attempt,
		CreatedAt:  msg.CreatedAt,
		ExpiresAt:  msg.ExpiresAt,
		SentAt:     msg.SentAt,
		Error:      msg.LastError,
	}
}

type listResponse struct {
	Messages   []messageResponse `json:"messages"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

func newListResponse(msgs []domain.Message, next uint64, ids *IDCodec) listResponse {
	out := make([]messageResponse, 0, len(msgs))
	for _, msg := range msgs {
		out = append(out, newMessageResponse(msg, ids))
	}
	resp := listResponse{Messages: out}
	if next != 0 {
		resp.NextCursor = ids.Encode(next)
	}
	return resp
}

type walletResponse struct {
	CustomerID uint32 `json:"customer_id"`
	Balance    int64  `json:"balance"`
	Blocked    int64  `json:"blocked"`
	Available  int64  `json:"available"`
}

func newWalletResponse(w domain.Wallet) walletResponse {
	return walletResponse{CustomerID: w.CustomerID, Balance: w.Balance, Blocked: w.Blocked, Available: w.Available()}
}

type errorResponse struct {
	Error string `json:"error"`
	Field string `json:"field,omitempty"`
}
