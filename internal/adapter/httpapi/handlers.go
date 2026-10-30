package httpapi

import (
	"github.com/gofiber/fiber/v3"
)

func (h *handler) createCustomer(c fiber.Ctx) error {
	var req createCustomerRequest
	if err := decode(c, &req); err != nil {
		return err
	}
	customer, err := h.svc.CreateCustomer(c.Context(), req.Name)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(newCustomerResponse(customer))
}

func (h *handler) getCustomer(c fiber.Ctx) error {
	id, err := pathCustomer(c)
	if err != nil {
		return err
	}
	customer, err := h.svc.Customer(c.Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(newCustomerResponse(customer))
}

func (h *handler) send(c fiber.Ctx) error {
	customerID, err := pathCustomer(c)
	if err != nil {
		return err
	}
	var req sendRequest
	if err := decode(c, &req); err != nil {
		return err
	}
	cmd, err := req.command(customerID)
	if err != nil {
		return err
	}
	msg, err := h.svc.Send(c.Context(), cmd)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusAccepted).JSON(newMessageResponse(msg, h.ids))
}

func (h *handler) getMessage(c fiber.Ctx) error {
	customerID, err := pathCustomer(c)
	if err != nil {
		return err
	}
	id, err := h.ids.Decode("message_id", c.Params("messageID"))
	if err != nil {
		return err
	}
	msg, err := h.svc.Message(c.Context(), customerID, id)
	if err != nil {
		return err
	}
	return c.JSON(newMessageResponse(msg, h.ids))
}

func (h *handler) getMessageByClientID(c fiber.Ctx) error {
	customerID, err := pathCustomer(c)
	if err != nil {
		return err
	}
	clientID, err := uuidParam("client_id", c.Params("clientID"))
	if err != nil {
		return err
	}
	msg, err := h.svc.MessageByClientID(c.Context(), customerID, clientID)
	if err != nil {
		return err
	}
	return c.JSON(newMessageResponse(msg, h.ids))
}

func (h *handler) listMessages(c fiber.Ctx) error {
	customerID, err := pathCustomer(c)
	if err != nil {
		return err
	}
	query, err := messageQuery(c, customerID, h.ids)
	if err != nil {
		return err
	}
	msgs, next, err := h.svc.Messages(c.Context(), query)
	if err != nil {
		return err
	}
	return c.JSON(newListResponse(msgs, next, h.ids))
}

func (h *handler) topUp(c fiber.Ctx) error {
	customerID, err := pathCustomer(c)
	if err != nil {
		return err
	}
	var req topUpRequest
	if err := decode(c, &req); err != nil {
		return err
	}
	wallet, err := h.svc.TopUp(c.Context(), customerID, req.Amount)
	if err != nil {
		return err
	}
	return c.JSON(newWalletResponse(wallet))
}

func (h *handler) balance(c fiber.Ctx) error {
	customerID, err := pathCustomer(c)
	if err != nil {
		return err
	}
	wallet, err := h.svc.Balance(c.Context(), customerID)
	if err != nil {
		return err
	}
	return c.JSON(newWalletResponse(wallet))
}
