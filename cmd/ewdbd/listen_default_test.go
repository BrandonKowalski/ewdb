//go:build !testlistener

package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ewdb/internal/session"
	"ewdb/internal/usb"
)

type fakeEndpoint struct{ name string }

func (fakeEndpoint) Next(context.Context) (session.Transport, error) {
	return nil, errors.New("unused")
}

func opener(results map[string]error) (func(string) (usb.Endpoint, error), *[]string) {
	var tried []string
	return func(spec string) (usb.Endpoint, error) {
		tried = append(tried, spec)
		if err, ok := results[spec]; ok && err != nil {
			return nil, err
		}
		if _, ok := results[spec]; !ok {
			return nil, errors.New("unexpected spec " + spec)
		}
		return fakeEndpoint{spec}, nil
	}, &tried
}

func testContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestOpenUSBFallsBackToAutoOnBadSpec(t *testing.T) {
	open, tried := opener(map[string]error{"bogus": fmt.Errorf("%w %q", usb.ErrBadSpec, "bogus"), "auto": nil})
	ep, used, err := openUSB(testContext(t), "bogus", open)
	if err != nil {
		t.Fatal(err)
	}
	if used != "auto" || ep.(fakeEndpoint).name != "auto" {
		t.Fatalf("used %q, tried %v", used, *tried)
	}
}

func TestOpenUSBFallsBackToAutoWhenConfiguredSpecFails(t *testing.T) {
	open, tried := opener(map[string]error{"ffs:/nope": errors.New("open ep0: no such file"), "auto": nil})
	ep, used, err := openUSB(testContext(t), "ffs:/nope", open)
	if err != nil {
		t.Fatal(err)
	}
	if used != "auto" || ep.(fakeEndpoint).name != "auto" {
		t.Fatalf("used %q, tried %v", used, *tried)
	}
}

func TestOpenUSBPrefersConfiguredSpec(t *testing.T) {
	open, tried := opener(map[string]error{"android:/dev/x": nil, "auto": nil})
	_, used, err := openUSB(testContext(t), "android:/dev/x", open)
	if err != nil {
		t.Fatal(err)
	}
	if used != "android:/dev/x" || len(*tried) != 1 {
		t.Fatalf("used %q, tried %v", used, *tried)
	}
}
