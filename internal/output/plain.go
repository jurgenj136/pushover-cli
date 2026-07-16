package output

import (
	"io"
	"strconv"
	"strings"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
)

type PlainRenderer struct{}

func (r PlainRenderer) Result(w io.Writer, result CommandResult) error {
	result = normalizeResult(result)
	if err := validateResult(result); err != nil {
		return err
	}
	if send, ok := result.(SendResult); ok && send.Status != deriveSendStatus(send.Batch) {
		return usage("output_status_mismatch", "send status does not match target outcomes")
	}
	var rows []string
	switch v := result.(type) {
	case SendResult:
		for _, target := range v.Batch.Results {
			code, category := "", ""
			if target.Error != nil {
				code, category = target.Error.Code, target.Error.Category
			}
			rows = append(rows, joinTSV(target.Alias, string(target.Outcome), strconv.Itoa(target.Priority), target.RequestID, target.ReceiptID, quotaText(target.QuotaRemaining), code, category))
		}
	case ProfilesResult:
		if v.Command != "profiles list" && v.Command != "profiles show" {
			return usage("plain_unsupported", "plain output is unsupported for this command")
		}
		for _, row := range v.Rows {
			rows = append(rows, joinTSV(row.Name, strconv.FormatBool(row.Default), strconv.Itoa(row.Priority), row.Title, row.Sound))
		}
	case RecipientsResult:
		for _, row := range v.Rows {
			rows = append(rows, joinTSVEscaped([]string{row.Alias, string(row.Kind), strconv.FormatBool(row.Validated), tsvList(row.Devices)}, 3))
		}
	case ValidationsResult:
		for _, row := range v.Rows {
			rows = append(rows, joinTSVEscaped([]string{row.Alias, strconv.FormatBool(row.Valid), tsvList(row.Devices), tsvList(row.Licenses), row.ErrorCode}, 2, 3))
		}
	case SoundsResult:
		for _, row := range v.Rows {
			rows = append(rows, joinTSV(row.Name, row.Description))
		}
	case LimitsResult:
		rows = append(rows, joinTSV(strconv.Itoa(v.Limit), strconv.Itoa(v.Remaining), strconv.FormatInt(v.Reset, 10)))
	case AuthStatusResult:
		rows = append(rows, joinTSV(v.Profile, v.Backend, strconv.FormatBool(v.Configured), strconv.FormatBool(v.TokenResolves)))
	case BackendResult:
		rows = append(rows, joinTSV(v.Backend))
	case InfoResult:
		rows = append(rows, joinTSV(v.ConfigPath, v.Profile, v.Backend, v.Version, v.Commit, v.BuildDate))
	case VersionResult:
		rows = append(rows, joinTSV(v.Version, v.Commit, v.BuildDate))
	default:
		return usage("plain_unsupported", "plain output is unsupported for this command")
	}
	if len(rows) == 0 {
		return nil
	}
	_, err := io.WriteString(w, strings.Join(rows, "\n")+"\n")
	return err
}

func (r PlainRenderer) Error(w io.Writer, command string, err *apperror.Error) error {
	if command == "" {
		return usage("output_command_missing", "command is missing")
	}
	if !knownCommand(command) {
		return usage("output_command_mismatch", "unknown command path")
	}
	detail := apperror.SafeDetail(err)
	_, writeErr := io.WriteString(w, "error: "+safeHuman(detail.Message)+"\n")
	return writeErr
}

func quotaText(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func joinTSV(values ...string) string {
	escaped := make([]string, len(values))
	for i, value := range values {
		escaped[i] = tsvEscape(value)
	}
	return strings.Join(escaped, "\t")
}

func joinTSVEscaped(values []string, already ...int) string {
	marked := make(map[int]bool, len(already))
	for _, index := range already {
		marked[index] = true
	}
	escaped := make([]string, len(values))
	for i, value := range values {
		if marked[i] {
			escaped[i] = value
		} else {
			escaped[i] = tsvEscape(value)
		}
	}
	return strings.Join(escaped, "\t")
}
