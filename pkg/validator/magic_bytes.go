package validator

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

var (
	ErrUnsupportedType = errors.New("unsupported media type: file must be a valid PDF, DOC or DOCX")
	ErrFileTooShort    = errors.New("file is too short to inspect magic bytes")
)

type DocumentType string

const (
	TypePDF  DocumentType = "application/pdf"
	TypeDOC  DocumentType = "application/msword"
	TypeDOCX DocumentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)

// Magic bytes signatures
var (
	pdfMagic = []byte{0x25, 0x50, 0x44, 0x46}                                     // %PDF
	docMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}             // OLE2 Compound File
	zipMagic = []byte{0x50, 0x4B, 0x03, 0x04}                                     // PK..
)

// InspectBytes inspects the initial byte slice to determine document type.
// If it matches a ZIP container, it requires InspectZip on the underlying file to confirm DOCX.
func InspectBytes(header []byte) (DocumentType, error) {
	if len(header) < 4 {
		return "", ErrFileTooShort
	}

	if bytes.HasPrefix(header, pdfMagic) {
		return TypePDF, nil
	}

	if len(header) >= len(docMagic) && bytes.HasPrefix(header, docMagic) {
		return TypeDOC, nil
	}

	if bytes.HasPrefix(header, zipMagic) {
		// Needs DOCX zip structure verification
		return TypeDOCX, nil
	}

	return "", ErrUnsupportedType
}

// ValidateFile opens a local file, inspects its magic bytes, and if it's a zip container,
// validates the internal OpenXML structure for DOCX ([Content_Types].xml and word/ folder).
func ValidateFile(filePath string) (DocumentType, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file for validation: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < 4 {
		return "", ErrUnsupportedType
	}

	header := make([]byte, 512)
	n, err := f.Read(header)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read file header: %w", err)
	}

	docType, err := InspectBytes(header[:n])
	if err != nil {
		return "", err
	}

	if docType == TypeDOCX {
		// Validate that the zip archive is a valid DOCX
		if err := validateDocxStructure(f, stat.Size()); err != nil {
			return "", err
		}
	}

	return docType, nil
}

// validateDocxStructure verifies that the zip file contains [Content_Types].xml and files under word/
func validateDocxStructure(ra io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return ErrUnsupportedType
	}

	hasContentTypes := false
	hasWordDir := false

	for _, file := range zr.File {
		cleaned := strings.TrimPrefix(file.Name, "/")
		if strings.EqualFold(cleaned, "[Content_Types].xml") {
			hasContentTypes = true
		}
		if strings.HasPrefix(strings.ToLower(cleaned), "word/") {
			hasWordDir = true
		}
	}

	if !hasContentTypes || !hasWordDir {
		return fmt.Errorf("%w: zip file does not conform to DOCX OpenXML structure", ErrUnsupportedType)
	}

	return nil
}
