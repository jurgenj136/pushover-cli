package output

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jurgenj136/pushover-cli/internal/apperror"
	"github.com/jurgenj136/pushover-cli/internal/domain"
)

type HumanRenderer struct{ Options RenderOptions }

func (r HumanRenderer) Result(w io.Writer, result CommandResult) error {
	result = normalizeResult(result)
	if err := validateResult(result); err != nil {
		return err
	}
	if send, ok := result.(SendResult); ok {
		if send.Status != deriveSendStatus(send.Batch) {
			return usage("output_status_mismatch", "send status does not match target outcomes")
		}
		for _, target := range send.Batch.Results {
			parts := []string{safeHuman(target.Alias), safeHuman(string(target.Outcome)), strconv.Itoa(target.Priority)}
			if target.RequestID != "" {
				parts = append(parts, "request="+safeHuman(target.RequestID))
			}
			if target.ReceiptID != "" {
				parts = append(parts, "receipt="+safeHuman(target.ReceiptID))
			}
			if target.QuotaRemaining != nil {
				parts = append(parts, "quota="+strconv.Itoa(*target.QuotaRemaining))
			}
			if target.Error != nil {
				parts = append(parts, safeHuman(target.Error.Code))
			}
			line := strings.Join(parts, " ")
			line = r.colorize(line, string(target.Outcome))
			if _, err := io.WriteString(w, line+"\n"); err != nil {
				return err
			}
		}
		return nil
	}
	if completion, ok := result.(CompletionResult); ok {
		_, err := io.WriteString(w, completion.Script)
		return err
	}
	lines := humanRows(result)
	if lines == nil {
		return usage("output_command_mismatch", "unsupported human result")
	}
	for _, line := range lines {
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}

func (r HumanRenderer) Error(w io.Writer, command string, err *apperror.Error) error {
	if command == "" {
		return usage("output_command_missing", "command is missing")
	}
	if !knownCommand(command) {
		return usage("output_command_mismatch", "unknown command path")
	}
	detail := apperror.SafeDetail(err)
	line := "error: " + safeHuman(detail.Message)
	if r.Options.Color && r.Options.TTY {
		line = "\x1b[31m" + line + "\x1b[0m"
	}
	_, writeErr := io.WriteString(w, line+"\n")
	return writeErr
}

func (r HumanRenderer) colorize(text, kind string) string {
	if !r.Options.Color || !r.Options.TTY {
		return text
	}
	code := ""
	switch kind {
	case string(domain.Sent):
		code = "32"
	case string(domain.Ambiguous):
		code = "33"
	case string(domain.Failed):
		code = "31"
	}
	if code == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func humanRows(result CommandResult) []string {
	var rows []string
	switch v := result.(type) {
	case ProfilesResult:
		for _, x := range v.Rows {
			rows = append(rows, fmt.Sprintf("%s %t %d %s %s", safeHuman(x.Name), x.Default, x.Priority, safeHuman(x.Title), safeHuman(x.Sound)))
		}
	case RecipientsResult:
		for _, x := range v.Rows {
			rows = append(rows, fmt.Sprintf("%s %s %t %s", safeHuman(x.Alias), safeHuman(string(x.Kind)), x.Validated, safeHuman(strings.Join(x.Devices, ","))))
		}
	case ValidationsResult:
		for _, x := range v.Rows {
			rows = append(rows, fmt.Sprintf("%s %t %s %s %s", safeHuman(x.Alias), x.Valid, safeHuman(strings.Join(x.Devices, ",")), safeHuman(strings.Join(x.Licenses, ",")), safeHuman(x.ErrorCode)))
		}
	case SoundsResult:
		for _, x := range v.Rows {
			rows = append(rows, safeHuman(x.Name)+" "+safeHuman(x.Description))
		}
	case LimitsResult:
		rows = []string{fmt.Sprintf("limit=%d remaining=%d reset=%d", v.Limit, v.Remaining, v.Reset)}
	case AuthStatusResult:
		rows = []string{fmt.Sprintf("profile=%s backend=%s configured=%t token_resolves=%t", safeHuman(v.Profile), safeHuman(v.Backend), v.Configured, v.TokenResolves)}
	case BackendResult:
		rows = []string{safeHuman(v.Backend)}
	case InfoResult:
		rows = []string{fmt.Sprintf("config_path=%s profile=%s backend=%s version=%s commit=%s build_date=%s features=%s", safeHuman(v.ConfigPath), safeHuman(v.Profile), safeHuman(v.Backend), safeHuman(v.Version), safeHuman(v.Commit), safeHuman(v.BuildDate), safeHuman(strings.Join(v.Features, ",")))}
	case VersionResult:
		rows = []string{fmt.Sprintf("version=%s commit=%s build_date=%s", safeHuman(v.Version), safeHuman(v.Commit), safeHuman(v.BuildDate))}
	case MutationResult:
		rows = []string{fmt.Sprintf("%s %s %s", safeHuman(v.Resource), safeHuman(v.Name), safeHuman(v.Action))}
	default:
		return nil
	}
	return rows
}
