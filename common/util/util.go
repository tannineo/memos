package util

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net/mail"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// ConvertStringToInt32 converts a string to int32.
func ConvertStringToInt32(src string) (int32, error) {
	i, err := strconv.Atoi(src)
	if err != nil {
		return 0, err
	}
	return int32(i), nil
}

// HasPrefixes returns true if the string s has any of the given prefixes.
func HasPrefixes(src string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(src, prefix) {
			return true
		}
	}
	return false
}

// ValidateEmail validates the email.
func ValidateEmail(email string) bool {
	if _, err := mail.ParseAddress(email); err != nil {
		return false
	}
	return true
}

func GenUUID() string {
	return uuid.New().String()
}

func Min(x, y int) int {
	if x < y {
		return x
	}
	return y
}

var letters = []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// RandomString returns a random string with length n.
func RandomString(n int) (string, error) {
	var sb strings.Builder
	sb.Grow(n)
	for i := 0; i < n; i++ {
		// The reason for using crypto/rand instead of math/rand is that
		// the former relies on hardware to generate random numbers and
		// thus has a stronger source of random numbers.
		randNum, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", err
		}
		if _, err := sb.WriteRune(letters[randNum.Uint64()]); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// ValidateRandomMemoSearchTags validates a search tags rule for random memo config
func ValidateRandomMemoSearchTags(s string) error {
	var trimmedString = strings.Replace(s, " ", "", -1)
	if trimmedString == "" {
		return nil
	}
	// prefix settings cannot start or end with comma
	if strings.HasPrefix(trimmedString, ",") || strings.HasSuffix(trimmedString, ",") {
		return fmt.Errorf("search tag settings cannot start or end with commas")
	}
	// if no characters other than comma, there is no valid tags
	if len(strings.Replace(trimmedString, ",", "", -1)) <= 0 {
		return fmt.Errorf("search tag settings must contain a valid tag")
	}
	return nil
}
