package resolver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

const maxCredentialsBytes = 4096

type suppliedCredentials struct {
	Credentials domain.Credentials
	E2EEKey     []byte
	Alias       string
}

func credentialsError(code, message string, category string) error {
	return apperror.New(code, category, message)
}

func parseCredentialsJSON(reader io.Reader) (suppliedCredentials, error) {
	if reader == nil {
		return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryUsage)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxCredentialsBytes+1))
	if err != nil {
		return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryValidation)
	}
	defer wipe(data)
	if len(data) > maxCredentialsBytes || !utf8.Valid(data) {
		return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryValidation)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryValidation)
	}
	values := map[string]string{}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || (name != "token" && name != "user_key" && name != "e2ee_key") || seen[name] {
			return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryValidation)
		}
		seen[name] = true
		var value string
		if err := decoder.Decode(&value); err != nil {
			return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryValidation)
		}
		values[name] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryValidation)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryValidation)
	}
	result := suppliedCredentials{Credentials: domain.Credentials{Token: values["token"], UserKey: values["user_key"]}, E2EEKey: []byte(values["e2ee_key"]), Alias: "stdin"}
	if err := domain.ValidateCredentials(result.Credentials); err != nil {
		return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryValidation)
	}
	if len(result.E2EEKey) > 0 {
		if err := domain.ValidateE2EEKey(result.E2EEKey); err != nil {
			return suppliedCredentials{}, credentialsError("credentials_invalid", "credentials input is invalid", apperror.CategoryValidation)
		}
	}
	return result, nil
}
