package research

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

const errMismatchFmt = "expected %v, got %v"

func TestValidateAnnotationRequest_InvalidUUID(t *testing.T) {
	req := annotationRequest{
		ItemID: "not-a-uuid",
		Rating: "good",
		Source: "web-list",
	}

	if err := validateAnnotationRequest(req); !errors.Is(err, errInvalidItemID) {
		t.Fatalf(errMismatchFmt, errInvalidItemID, err)
	}
}

func TestValidateAnnotationRequest_Valid(t *testing.T) {
	req := annotationRequest{
		ItemID: uuid.NewString(),
		Rating: "good",
		Source: "web-list",
	}

	if err := validateAnnotationRequest(req); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateAnnotationBatch_InvalidUUID(t *testing.T) {
	req := annotationBatchRequest{
		ItemIDs: []string{uuid.NewString(), "bad"},
		Rating:  "bad",
		Source:  "web-expanded",
	}

	if err := validateAnnotationBatch(req); !errors.Is(err, errInvalidItemID) {
		t.Fatalf(errMismatchFmt, errInvalidItemID, err)
	}
}

func TestValidateAnnotationBatch_TooMany(t *testing.T) {
	ids := make([]string, annotationMaxBatch+1)
	for i := range ids {
		ids[i] = uuid.NewString()
	}

	req := annotationBatchRequest{
		ItemIDs: ids,
		Rating:  "good",
		Source:  "web-list",
	}

	if err := validateAnnotationBatch(req); !errors.Is(err, errTooManyItemIDs) {
		t.Fatalf(errMismatchFmt, errTooManyItemIDs, err)
	}
}

func TestValidateAnnotationRequest_InvalidRating(t *testing.T) {
	req := annotationRequest{
		ItemID: uuid.NewString(),
		Rating: "meh",
		Source: "web-list",
	}

	if err := validateAnnotationRequest(req); !errors.Is(err, errInvalidRating) {
		t.Fatalf(errMismatchFmt, errInvalidRating, err)
	}
}
