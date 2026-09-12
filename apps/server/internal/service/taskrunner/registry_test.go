package taskrunner_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/service/taskrunner"
)

func TestRegistryRejectsDuplicateAndMissingHandlers(t *testing.T) {
	def := taskrunner.Definition{
		Type: "assessment", MaxAttempts: 3, Timeout: 90 * time.Second,
		LeaseDuration: 30 * time.Second, HeartbeatEvery: 10 * time.Second,
		Concurrency: 2, RetryBackoff: taskrunner.ExponentialBackoff(time.Second, time.Minute),
	}
	_, err := taskrunner.NewRegistry([]taskrunner.Definition{def, def}, []taskrunner.Handler{fakeHandler{taskType: "assessment"}})
	if err == nil || !strings.Contains(err.Error(), "duplicate task definition") {
		t.Fatalf("duplicate definitions error = %v, want duplicate task definition", err)
	}
	_, err = taskrunner.NewRegistry([]taskrunner.Definition{def}, nil)
	if err == nil || !strings.Contains(err.Error(), "missing handler") {
		t.Fatalf("missing handler error = %v, want missing handler", err)
	}
}

func TestRegistryRejectsUnregisteredHandlerAndInvalidPolicy(t *testing.T) {
	valid := validDefinition("assessment")
	_, err := taskrunner.NewRegistry([]taskrunner.Definition{valid}, []taskrunner.Handler{
		fakeHandler{taskType: "assessment"},
		fakeHandler{taskType: "render"},
	})
	if err == nil || !strings.Contains(err.Error(), "unregistered handler") {
		t.Fatalf("unregistered handler error = %v", err)
	}

	cases := []struct {
		name string
		def  taskrunner.Definition
	}{
		{name: "max attempts", def: withDef(valid, func(d *taskrunner.Definition) { d.MaxAttempts = 0 })},
		{name: "timeout", def: withDef(valid, func(d *taskrunner.Definition) { d.Timeout = d.HeartbeatEvery })},
		{name: "lease", def: withDef(valid, func(d *taskrunner.Definition) { d.LeaseDuration = 2*d.HeartbeatEvery - time.Nanosecond })},
		{name: "concurrency", def: withDef(valid, func(d *taskrunner.Definition) { d.Concurrency = 0 })},
	}
	for _, tc := range cases {
		_, err := taskrunner.NewRegistry([]taskrunner.Definition{tc.def}, []taskrunner.Handler{fakeHandler{taskType: tc.def.Type}})
		if err == nil {
			t.Fatalf("%s: accepted invalid definition", tc.name)
		}
	}
}

func TestRegistryExposesImmutableLookups(t *testing.T) {
	def := validDefinition("assessment")
	reg, err := taskrunner.NewRegistry([]taskrunner.Definition{def}, []taskrunner.Handler{fakeHandler{taskType: "assessment"}})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	types := reg.Types()
	if len(types) != 1 || types[0] != "assessment" {
		t.Fatalf("Types() = %v", types)
	}
	types[0] = "mutated"
	if got := reg.Types(); len(got) != 1 || got[0] != "assessment" {
		t.Fatalf("Types() mutated to %v", got)
	}
	got, ok := reg.Definition("assessment")
	if !ok || got.Type != "assessment" || got.MaxAttempts != 3 {
		t.Fatalf("Definition = %+v ok=%v", got, ok)
	}
	handler, ok := reg.Handler("assessment")
	if !ok || handler.Type() != "assessment" {
		t.Fatalf("Handler ok=%v type=%v", ok, handler)
	}
	if _, ok := reg.Definition("render"); ok {
		t.Fatal("Definition(render) should miss")
	}
}

func TestExponentialBackoffCapsAtMax(t *testing.T) {
	backoff := taskrunner.ExponentialBackoff(time.Second, 4*time.Second)
	if got := backoff(1); got != time.Second {
		t.Fatalf("attempt 1 = %s, want 1s", got)
	}
	if got := backoff(2); got != 2*time.Second {
		t.Fatalf("attempt 2 = %s, want 2s", got)
	}
	if got := backoff(3); got != 4*time.Second {
		t.Fatalf("attempt 3 = %s, want 4s", got)
	}
	if got := backoff(4); got != 4*time.Second {
		t.Fatalf("attempt 4 = %s, want 4s", got)
	}
}

func TestTaskErrorIsRecoverableWithErrorsAs(t *testing.T) {
	err := &taskrunner.TaskError{Class: domain.ErrorThrottled, Code: "rate_limited"}
	wrapped := errors.Join(errors.New("provider"), err)
	var got *taskrunner.TaskError
	if !errors.As(wrapped, &got) {
		t.Fatal("errors.As did not recover TaskError")
	}
	if got.Class != domain.ErrorThrottled || got.Code != "rate_limited" {
		t.Fatalf("recovered %+v", got)
	}
}

func validDefinition(taskType domain.TaskType) taskrunner.Definition {
	return taskrunner.Definition{
		Type: taskType, MaxAttempts: 3, Timeout: 90 * time.Second,
		LeaseDuration: 30 * time.Second, HeartbeatEvery: 10 * time.Second,
		Concurrency: 2, RetryBackoff: taskrunner.ExponentialBackoff(time.Second, time.Minute),
	}
}

func withDef(def taskrunner.Definition, edit func(*taskrunner.Definition)) taskrunner.Definition {
	edit(&def)
	return def
}
