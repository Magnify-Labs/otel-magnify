package ext

import (
	"context"
	"strings"
	"testing"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

func TestNopReportSignerUsesCommunityNoneSignature(t *testing.T) {
	signer := NopReportSigner{}
	payloadHash := strings.Repeat("a", 64)

	sig, err := signer.SignReport(context.Background(), payloadHash, []byte(`{"schema_version":"evidence_pack.v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if sig.Scheme != models.ReportSignatureSchemeNone {
		t.Fatalf("scheme = %q, want %q", sig.Scheme, models.ReportSignatureSchemeNone)
	}
	if sig.PayloadHash != payloadHash {
		t.Fatalf("payload hash = %q, want %q", sig.PayloadHash, payloadHash)
	}
	if sig.Verifier != "community-none" {
		t.Fatalf("verifier = %q, want community-none", sig.Verifier)
	}
	if sig.SignedAt.IsZero() {
		t.Fatal("signed_at must be populated for report metadata")
	}

	verification, err := signer.VerifyReport(context.Background(), []byte(`{"schema_version":"evidence_pack.v1"}`), sig)
	if err != nil {
		t.Fatal(err)
	}
	if !verification.Valid || verification.Scheme != models.ReportSignatureSchemeNone || verification.PayloadHash != payloadHash || verification.Verifier != "community-none" {
		t.Fatalf("unexpected verification result: %#v", verification)
	}
}
