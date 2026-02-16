package database

import (
	"founders-toolkit-api/models"
	"log"
	"os"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Service struct {
	DB *gorm.DB
}

func New() *Service {
	dsn := os.Getenv("DB_URL")

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatal("failed to connect database: ", err)
	}

	return &Service{DB: db}
}

func (s *Service) CreateUser(user *models.User) error {
	return s.DB.Create(user).Error
}

func (s *Service) FindUserById(id string) (models.User, error) {
	userPtr := &models.User{}
	err := s.DB.First(userPtr, id).Error
	return *userPtr, err
}

func (s *Service) FindUserByEmail(email string) (models.User, error) {
	userPtr := &models.User{}
	err := s.DB.First(userPtr, "email = ?", email).Error
	return *userPtr, err
}

func (s *Service) GetAllUsers() ([]models.User, error) {
	var users []models.User
	err := s.DB.Find(&users).Error
	return users, err
}

func (s *Service) UpdateUser(user models.User) (models.User, error) {
	if err := s.DB.First(&user, user.ID).Error; err != nil {
		return user, err
	}
	return user, s.DB.Model(&user).Updates(&user).Error
}

func (s *Service) DeleteUser(user models.User) (models.User, error) {
	if err := s.DB.First(&user, user.ID).Error; err != nil {
		return user, err
	}
	return user, s.DB.Delete(&user).Error
}
