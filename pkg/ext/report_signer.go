package ext

import (
	"context"
	"time"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// ReportSigner signs and verifies canonical evidence-pack payloads. Community
// builds use NopReportSigner; enterprise builds can provide HMAC/Ed25519
// implementations without importing enterprise code into the community module.
type ReportSigner interface {
	SignReport(ctx context.Context, payloadHash string, canonicalPayload []byte) (models.ReportSignature, error)
	VerifyReport(ctx context.Context, canonicalPayload []byte, sig models.ReportSignature) (models.ReportVerification, error)
}

// NopReportSigner records community "unsigned" metadata while preserving the
// same response shape as enterprise-signed reports.
type NopReportSigner struct{}

// SignReport returns unsigned community report metadata for the payload hash.
func (NopReportSigner) SignReport(_ context.Context, payloadHash string, _ []byte) (models.ReportSignature, error) {
	return models.ReportSignature{
		Scheme:      models.ReportSignatureSchemeNone,
		SignedAt:    time.Now().UTC(),
		PayloadHash: payloadHash,
		Verifier:    models.ReportSignatureVerifierCommunityNone,
	}, nil
}

// VerifyReport validates community unsigned report metadata against the provided signature.
func (NopReportSigner) VerifyReport(_ context.Context, _ []byte, sig models.ReportSignature) (models.ReportVerification, error) {
	return models.ReportVerification{
		Valid:       sig.Scheme == models.ReportSignatureSchemeNone,
		Scheme:      models.ReportSignatureSchemeNone,
		PayloadHash: sig.PayloadHash,
		Verifier:    models.ReportSignatureVerifierCommunityNone,
		CheckedAt:   time.Now().UTC(),
	}, nil
}
