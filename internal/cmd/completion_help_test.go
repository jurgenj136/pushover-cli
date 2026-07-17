package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jurgenj136/pushover-cli/internal/delivery"
	"github.com/jurgenj136/pushover-cli/internal/operation"
	"github.com/jurgenj136/pushover-cli/internal/preflight"
)

func TestStaticHelpAndCompletionDoNotOpenRuntime(t *testing.T) {
	runtimeCalls, adminCalls := 0, 0
	deps := Dependencies{
		runtime: func(context.Context, operation.Scope) (preflight.Builder, delivery.Runner, error) {
			runtimeCalls++
			return preflight.Builder{}, delivery.Runner{}, nil
		},
		admin: func(context.Context) (AdminDependencies, error) {
			adminCalls++
			return AdminDependencies{}, nil
		},
	}

	for _, args := range [][]string{{"--help"}, {"auth", "--help"}, {"auth", "backend", "--help"}, {"completion", "fish"}} {
		var stdout, stderr bytes.Buffer
		if exit := Execute(context.Background(), args, testIO(&stdout, &stderr), deps); exit != 0 {
			t.Fatalf("args=%q exit=%d stderr=%q", args, exit, stderr.String())
		}
		if stderr.Len() != 0 {
			t.Fatalf("args=%q stderr=%q, want empty", args, stderr.String())
		}
	}
	if runtimeCalls != 0 || adminCalls != 0 {
		t.Fatalf("runtime calls=%d admin calls=%d, want zero", runtimeCalls, adminCalls)
	}
}

func TestHelpExposesBareAuthBackendWithoutShow(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"auth", "--help"}, {"auth", "backend", "--help"}} {
		var stdout, stderr bytes.Buffer
		if exit := Execute(context.Background(), args, testIO(&stdout, &stderr), Dependencies{}); exit != 0 {
			t.Fatalf("args=%q exit=%d stderr=%q", args, exit, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Run 'pushover auth backend'") || strings.Contains(stdout.String(), "auth backend show") {
			t.Fatalf("args=%q help=%q", args, stdout.String())
		}
	}
}
