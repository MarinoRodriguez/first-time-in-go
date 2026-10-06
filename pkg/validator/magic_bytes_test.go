package validator

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectBytes(t *testing.T) {
	// PDF
	pdfBytes := []byte("%PDF-1.7 header")
	docType, err := InspectBytes(pdfBytes)
	if err != nil || docType != TypePDF {
		t.Fatalf("expected PDF, got %v (err: %v)", docType, err)
	}

	// DOC
	docBytes := []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1, 0x00, 0x00}
	docType, err = InspectBytes(docBytes)
	if err != nil || docType != TypeDOC {
		t.Fatalf("expected DOC, got %v (err: %v)", docType, err)
	}

	// DOCX (ZIP magic)
	zipBytes := []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0x00}
	docType, err = InspectBytes(zipBytes)
	if err != nil || docType != TypeDOCX {
		t.Fatalf("expected DOCX candidate, got %v (err: %v)", docType, err)
	}

	// Unsupported (e.g. plain text or executable)
	txtBytes := []byte("Hello world, this is a plain text file")
	_, err = InspectBytes(txtBytes)
	if err != ErrUnsupportedType {
		t.Fatalf("expected ErrUnsupportedType, got %v", err)
	}
}

func TestValidateFile_PDF(t *testing.T) {
	tmpDir := t.TempDir()
	pdfFile := filepath.Join(tmpDir, "sample.pdf")
	if err := os.WriteFile(pdfFile, []byte("%PDF-1.4\n%âãÏÓ\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF"), 0644); err != nil {
		t.Fatal(err)
	}

	docType, err := ValidateFile(pdfFile)
	if err != nil {
		t.Fatalf("unexpected error validating PDF: %v", err)
	}
	if docType != TypePDF {
		t.Fatalf("expected %s, got %s", TypePDF, docType)
	}
}

func TestValidateFile_DOC(t *testing.T) {
	tmpDir := t.TempDir()
	docFile := filepath.Join(tmpDir, "sample.doc")
	content := append(docMagic, []byte("legacy word contents")...)
	if err := os.WriteFile(docFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	docType, err := ValidateFile(docFile)
	if err != nil {
		t.Fatalf("unexpected error validating DOC: %v", err)
	}
	if docType != TypeDOC {
		t.Fatalf("expected %s, got %s", TypeDOC, docType)
	}
}

func TestValidateFile_DOCX_ValidAndInvalid(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Valid DOCX with [Content_Types].xml and word/document.xml
	validDocx := filepath.Join(tmpDir, "valid.docx")
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)

	ctW, _ := zw.Create("[Content_Types].xml")
	_, _ = ctW.Write([]byte("<Types></Types>"))

	docW, _ := zw.Create("word/document.xml")
	_, _ = docW.Write([]byte("<w:document></w:document>"))

	_ = zw.Close()
	if err := os.WriteFile(validDocx, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	docType, err := ValidateFile(validDocx)
	if err != nil {
		t.Fatalf("expected valid DOCX, got error: %v", err)
	}
	if docType != TypeDOCX {
		t.Fatalf("expected %s, got %s", TypeDOCX, docType)
	}

	// 2. Regular ZIP without word/ structure (should be rejected)
	invalidDocx := filepath.Join(tmpDir, "fake.docx")
	buf2 := new(bytes.Buffer)
	zw2 := zip.NewWriter(buf2)
	otherW, _ := zw2.Create("test.txt")
	_, _ = otherW.Write([]byte("just a zip file"))
	_ = zw2.Close()
	if err := os.WriteFile(invalidDocx, buf2.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = ValidateFile(invalidDocx)
	if err == nil {
		t.Fatal("expected error for non-DOCX zip, got nil")
	}
}
