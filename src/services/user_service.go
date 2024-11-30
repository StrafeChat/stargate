package services

import (
	"github.com/StrafeChat/stargate/src/repository"
)

type UserService struct {
	repo *repository.UserRepository
}

func NewUserService(repo *repository.UserRepository) *UserService {
	return &UserService{
		repo: repo,
	}
}

func (s *UserService) ValidateToken(token string) (string, error) {
	return s.repo.ValidateSessionToken(token)
}

func (s *UserService) GetUserDetails(userID string) (repository.UserDetails, error) {
	return s.repo.GetUserDetails(userID)
}

func (s *UserService) SetUserOnline(userID string) error {
	return s.repo.SetUserOnline(userID)
}

func (s *UserService) SetUserOffline(userID string, sessionToken string) error {
	return s.repo.SetUserOffline(userID, sessionToken)
}
