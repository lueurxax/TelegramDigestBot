package mocks

import (
	"context"
	"errors"
	"testing"
)

const (
	testKey          = "test_key"
	testValue        = "test_value"
	testKeyMissing   = "nonexistent"
	settingsKey      = "key"
	settingsValue    = "value"
	settingsKeyOne   = "key1"
	settingsValueOne = "value1"
	settingsKeyTwo   = "key2"
	settingsValueTwo = "value2"

	errFmtGetSetting = "GetSetting() error = %v"
	errFmtGetResult  = "GetSetting() = %v, want %v"
)

// errCustomTest is a static error for testing custom function overrides.
var errCustomTest = errors.New("custom test error")

func TestSettingsStore_GetSet(t *testing.T) {
	store := NewSettingsStore()
	ctx := context.Background()

	// Test setting a value
	store.Set(testKey, testValue)

	var result string

	err := store.GetSetting(ctx, testKey, &result)
	if err != nil {
		t.Fatalf(errFmtGetSetting, err)
	}

	if result != testValue {
		t.Errorf(errFmtGetResult, result, testValue)
	}
}

func TestSettingsStore_GetNotFound(t *testing.T) {
	store := NewSettingsStore()
	ctx := context.Background()

	var result string

	err := store.GetSetting(ctx, testKeyMissing, &result)

	if !errors.Is(err, ErrSettingNotFound) {
		t.Errorf("GetSetting() error = %v, want %v", err, ErrSettingNotFound)
	}
}

func TestSettingsStore_SaveWithHistory(t *testing.T) {
	store := NewSettingsStore()
	ctx := context.Background()

	err := store.SaveSettingWithHistory(ctx, settingsKey, settingsValue, 123)
	if err != nil {
		t.Fatalf("SaveSettingWithHistory() error = %v", err)
	}

	var result string

	err = store.GetSetting(ctx, settingsKey, &result)
	if err != nil {
		t.Fatalf(errFmtGetSetting, err)
	}

	if result != settingsValue {
		t.Errorf(errFmtGetResult, result, settingsValue)
	}
}

func TestSettingsStore_Delete(t *testing.T) {
	store := NewSettingsStore()
	ctx := context.Background()

	store.Set(settingsKey, settingsValue)

	err := store.DeleteSettingWithHistory(ctx, settingsKey, 123)
	if err != nil {
		t.Fatalf("DeleteSettingWithHistory() error = %v", err)
	}

	var result string

	err = store.GetSetting(ctx, settingsKey, &result)

	if !errors.Is(err, ErrSettingNotFound) {
		t.Errorf("GetSetting() after delete error = %v, want %v", err, ErrSettingNotFound)
	}
}

func TestSettingsStore_CustomFn(t *testing.T) {
	store := NewSettingsStore()
	ctx := context.Background()

	store.GetSettingFn = func(_ context.Context, _ string, _ interface{}) error {
		return errCustomTest
	}

	var result string

	err := store.GetSetting(ctx, "any", &result)

	if !errors.Is(err, errCustomTest) {
		t.Errorf("GetSetting() with custom fn error = %v, want %v", err, errCustomTest)
	}
}

func TestSettingsStore_Clear(t *testing.T) {
	store := NewSettingsStore()
	ctx := context.Background()

	store.Set(settingsKeyOne, settingsValueOne)
	store.Set(settingsKeyTwo, settingsValueTwo)
	store.Clear()

	var result string

	err := store.GetSetting(ctx, settingsKeyOne, &result)

	if !errors.Is(err, ErrSettingNotFound) {
		t.Errorf("GetSetting() after clear error = %v, want %v", err, ErrSettingNotFound)
	}
}
