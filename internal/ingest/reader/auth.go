package reader

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
)

// ErrSignupNotSupported indicates that signup is not supported.
var ErrSignupNotSupported = errors.New("signup not supported")

func (r *Reader) authFlow() auth.Flow {
	return auth.NewFlow(r, auth.SendCodeOptions{})
}

func (r *Reader) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	fmt.Print("Enter code: ")

	code, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read auth code: %w", err)
	}

	return strings.TrimSpace(code), nil
}

func (r *Reader) Phone(_ context.Context) (string, error) {
	var phone string

	var err error

	if r.cfg.TGPhone != "" {
		phone = r.cfg.TGPhone
	} else {
		fmt.Print("Enter phone: ")

		phone, err = bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read phone number: %w", err)
		}
	}

	phone = r.sanitizePhone(phone)
	r.logger.Info().Str("phone", r.maskPhone(phone)).Msg("Using phone number")

	if len(phone) < 10 {
		r.logger.Warn().Int("length", len(phone)).Msg("Phone number seems too short, it might be invalid. Ensure it includes country code (e.g. +1...)")
	}

	return phone, nil
}

func (r *Reader) Password(_ context.Context) (string, error) {
	var password string

	var err error

	if r.cfg.TG2FAPassword != "" {
		password = r.cfg.TG2FAPassword
	} else {
		fmt.Print("Enter 2FA password: ")

		password, err = bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read 2FA password: %w", err)
		}
	}

	return strings.TrimSpace(password), nil
}

func (r *Reader) AcceptTermsOfService(_ context.Context, _ tg.HelpTermsOfService) error {
	return nil
}

func (r *Reader) SignUp(_ context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, ErrSignupNotSupported
}
