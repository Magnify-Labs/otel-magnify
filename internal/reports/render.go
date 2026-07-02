package reports

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"sort"
	"strings"

	"github.com/magnify-labs/otel-magnify/pkg/models"
)

// RenderMarkdown renders an evidence pack as deterministic Markdown.
func RenderMarkdown(pack models.EvidencePack) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# Evidence Pack\n\n")
	fmt.Fprintf(&b, "- schema_version: `%s`\n- generated_at: `%s`\n- inputs_hash: `%s`\n- report_hash: `%s`\n\n", pack.SchemaVersion, pack.GeneratedAt.UTC().Format(timeRFC3339Nano), pack.InputsHash, pack.ReportHash)
	if len(pack.Warnings) > 0 {
		b.WriteString("## Warnings\n\n")
		for _, w := range pack.Warnings {
			fmt.Fprintf(&b, "- `%s`: %s\n", w.Code, w.Message)
		}
		b.WriteString("\n")
	}
	sections := append([]models.EvidenceSection(nil), pack.Sections...)
	sort.Slice(sections, func(i, j int) bool { return sections[i].Order < sections[j].Order })
	for _, sec := range sections {
		fmt.Fprintf(&b, "## %s\n\n", sec.Title)
		items := append([]models.EvidenceItem(nil), sec.Items...)
		sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		for _, item := range items {
			fmt.Fprintf(&b, "- `%s` %s", item.ID, item.Summary)
			if item.ObservedAt != nil {
				fmt.Fprintf(&b, " (%s)", item.ObservedAt.UTC().Format(timeRFC3339Nano))
			}
			b.WriteString("\n")
			keys := sortedFactKeys(item.Facts)
			for _, k := range keys {
				fmt.Fprintf(&b, "  - %s: `%s`\n", k, canonicalScalar(item.Facts[k]))
			}
		}
		b.WriteString("\n")
	}
	if len(pack.Signatures) > 0 {
		b.WriteString("## Signatures\n\n")
		for _, sig := range pack.Signatures {
			fmt.Fprintf(&b, "- scheme: `%s`, verifier: `%s`, payload_hash: `%s`, key_id: `%s`\n", sig.Scheme, sig.Verifier, sig.PayloadHash, sig.KeyID)
		}
	}
	return []byte(b.String()), nil
}

const timeRFC3339Nano = "2006-01-02T15:04:05.999999999Z07:00"

// RenderCSV renders an evidence pack as a deterministic flat CSV table.
func RenderCSV(pack models.EvidencePack) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	cols := []string{"section_id", "item_id", "resource", "resource_id", "observed_at", "severity", "summary", "key", "value", "content_hash", "redacted"}
	_ = w.Write(cols)
	sections := append([]models.EvidenceSection(nil), pack.Sections...)
	sort.Slice(sections, func(i, j int) bool { return sections[i].Order < sections[j].Order })
	for _, sec := range sections {
		items := append([]models.EvidenceItem(nil), sec.Items...)
		sort.SliceStable(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		for _, item := range items {
			keys := sortedFactKeys(item.Facts)
			if len(keys) == 0 {
				keys = []string{""}
			}
			for _, k := range keys {
				observed := ""
				if item.ObservedAt != nil {
					observed = item.ObservedAt.UTC().Format(timeRFC3339Nano)
				}
				value := ""
				if k != "" {
					value = canonicalScalar(item.Facts[k])
				}
				_ = w.Write([]string{sec.ID, item.ID, item.Resource, item.ResourceID, observed, item.Severity, item.Summary, k, value, item.ContentHash, fmt.Sprintf("%t", item.Redacted)})
			}
		}
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// RenderPDFMinimal renders a deterministic text-only PDF using the standard library.
func RenderPDFMinimal(pack models.EvidencePack) ([]byte, error) {
	md, _ := RenderMarkdown(pack)
	text := strings.ReplaceAll(string(md), "\\", "\\\\")
	text = strings.ReplaceAll(text, "(", "\\(")
	text = strings.ReplaceAll(text, ")", "\\)")
	lines := strings.Split(text, "\n")
	var stream strings.Builder
	stream.WriteString("BT /F1 10 Tf 50 780 Td ")
	for i, line := range lines {
		if i > 0 {
			stream.WriteString("0 -14 Td ")
		}
		if len(line) > 100 {
			line = line[:100]
		}
		fmt.Fprintf(&stream, "(%s) Tj ", line)
		if i > 48 {
			break
		}
	}
	stream.WriteString("ET")
	objects := []string{
		"1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n",
		"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n",
		"3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >> endobj\n",
		"4 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\n",
		fmt.Sprintf("5 0 obj << /Length %d >> stream\n%s\nendstream endobj\n", len(stream.String()), stream.String()),
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for _, obj := range objects {
		offsets = append(offsets, b.Len())
		b.WriteString(obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for i := 1; i < len(offsets); i++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&b, "trailer << /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(b.String()), nil
}

func sortedFactKeys(facts map[string]any) []string {
	keys := make([]string, 0, len(facts))
	for k := range facts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
