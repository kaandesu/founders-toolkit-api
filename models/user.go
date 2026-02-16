package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

type StringArray []string

func (s *StringArray) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan StringArray: %v", value)
	}
	return json.Unmarshal(b, s)
}

func (s StringArray) Value() (driver.Value, error) {
	return json.Marshal(s)
}

type User struct {
	ID        int64     `json:"id,omitempty" gorm:"column:id;primaryKey;autoIncrement"`
	Email     string    `json:"email,omitempty" gorm:"column:email;uniqueIndex;not null"`
	Fullname  string    `json:"full_name,omitempty" gorm:"column:full_name;"`
	Password  string    `json:"password,omitempty" gorm:"column:password"`
	CreatedAt time.Time `json:"created_at,omitempty" gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at,omitempty" gorm:"column:updated_at;autoUpdateTime"`
}

func (User) TableName() string { return "users" }

type JSONB json.RawMessage

func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}

	b, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to scan JSONB: %v", value)
	}

	*j = JSONB(b)
	return nil
}

func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return []byte(j), nil
}

func (j JSONB) MarshalJSON() ([]byte, error) {
	return json.RawMessage(j).MarshalJSON()
}

func (j JSONB) UnmarshalTo(v interface{}) error {
	if j == nil {
		return fmt.Errorf("JSONB is nil")
	}
	return json.Unmarshal(j, v)
}
