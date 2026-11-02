package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/lcmps/retroxide/application/fault"
	authdomain "github.com/lcmps/retroxide/domain/auth"
	"github.com/lcmps/retroxide/domain/identity"
)

// ClientService manages long-lived API client tokens owned by administrators.
type ClientService struct {
	clients ClientRepository
}

func NewClientService(clients ClientRepository) *ClientService {
	return &ClientService{clients: clients}
}

func (service *ClientService) List(ctx context.Context, requester identity.Requester) (ClientListResponse, error) {
	if !requester.IsAdmin() {
		return ClientListResponse{}, fault.Forbidden()
	}
	clients, err := service.clients.FindByUserID(ctx, requester.UserID)
	if err != nil {
		return ClientListResponse{}, err
	}
	result := ClientListResponse{Item: make([]ClientDTO, 0, len(clients))}
	for _, client := range clients {
		result.Item = append(result.Item, ClientDTO{ID: client.ID, UserID: client.UserID, Name: client.Name})
	}
	return result, nil
}

func (service *ClientService) Create(ctx context.Context, requester identity.Requester, name string) (CreateClientResponse, error) {
	if !requester.IsAdmin() {
		return CreateClientResponse{}, fault.Forbidden()
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return CreateClientResponse{}, fault.Internal(fmt.Errorf("generating client token: %w", err))
	}
	clientIDBytes := make([]byte, 16)
	if _, err := rand.Read(clientIDBytes); err != nil {
		return CreateClientResponse{}, fault.Internal(fmt.Errorf("generating client id: %w", err))
	}
	token := "rtx_" + hex.EncodeToString(tokenBytes)
	tokenHash := sha256.Sum256([]byte(token))
	client := authdomain.Client{
		ID:        hex.EncodeToString(clientIDBytes),
		UserID:    requester.UserID,
		Name:      name,
		TokenHash: hex.EncodeToString(tokenHash[:]),
	}
	if err := service.clients.SaveClient(ctx, client); err != nil {
		return CreateClientResponse{}, err
	}
	return CreateClientResponse{ID: client.ID, Name: client.Name, Token: token}, nil
}

func (service *ClientService) Revoke(ctx context.Context, requester identity.Requester, clientID string) error {
	if !requester.IsAdmin() {
		return fault.Forbidden()
	}
	return service.clients.DeleteClient(ctx, clientID)
}
