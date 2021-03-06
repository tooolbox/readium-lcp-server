// Copyright 2020 Readium Foundation. All rights reserved.
// Use of this source code is governed by a BSD-style license
// that can be found in the LICENSE file exposed on Github (readium) in the project repository.

package pack

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"text/template"

	"github.com/readium/readium-lcp-server/license"
	"github.com/readium/readium-lcp-server/rwpm"
)

// RWPPReader is a Readium Package reader
type RWPPReader struct {
	manifest   rwpm.Publication
	zipArchive *zip.Reader
}

// RWPPWriter is a REadium Package writer
type RWPPWriter struct {
	manifest  rwpm.Publication
	zipWriter *zip.Writer
}

// NopWriteCloser object
type NopWriteCloser struct {
	io.Writer
}

// NewWriter returns a new PackageWriter writing a RWP to the output file
func (reader *RWPPReader) NewWriter(writer io.Writer) (PackageWriter, error) {

	zipWriter := zip.NewWriter(writer)

	files := map[string]*zip.File{}
	for _, file := range reader.zipArchive.File {
		files[file.Name] = file
	}

	// copy immediately the W3C manifest if it exists in the source package
	if w3cmanFile, ok := files[W3CManifestName]; ok {
		fw, err := zipWriter.Create(W3CManifestName)
		if err != nil {
			return nil, err
		}
		file, err := w3cmanFile.Open()
		_, err = io.Copy(fw, file)
		file.Close()
	}

	// copy immediately the ancilliary resources from the source manifest as they should not be encrypted
	// FIXME: this doesn't seem to be the best location for such zip to zip copy
	for _, manifestResource := range reader.manifest.Resources {
		sourceFile := files[manifestResource.Href]
		fw, err := zipWriter.Create(sourceFile.Name)
		if err != nil {
			return nil, err
		}
		file, err := sourceFile.Open()
		_, err = io.Copy(fw, file)
		file.Close()
	}

	manifest := reader.manifest
	manifest.ReadingOrder = nil

	return &RWPPWriter{
		zipWriter: zipWriter,
		manifest:  manifest,
	}, nil
}

// Resources returns a list of all resources which should be encrypted
// FIXME: the name of this function isn't great.
// Note: the current design choice is to leave ancillaty resources (in "resources") non-encrypted
// FIXME: also encrypt "resources" and "alternates"
func (reader *RWPPReader) Resources() []Resource {
	// index files by name to avoid multiple linear searches
	files := map[string]*zip.File{}
	for _, file := range reader.zipArchive.File {
		files[file.Name] = file
	}

	// list files from the reading order; keep their type and encryption status
	var resources []Resource
	for _, manifestResource := range reader.manifest.ReadingOrder {
		isEncrypted := manifestResource.Properties != nil && manifestResource.Properties.Encrypted != nil
		resources = append(resources, &rwpResource{file: files[manifestResource.Href], isEncrypted: isEncrypted, contentType: manifestResource.Type})
	}

	return resources
}

type rwpResource struct {
	isEncrypted bool
	contentType string
	file        *zip.File
}

func (resource *rwpResource) Path() string                   { return resource.file.Name }
func (resource *rwpResource) ContentType() string            { return resource.contentType }
func (resource *rwpResource) Size() int64                    { return int64(resource.file.UncompressedSize64) }
func (resource *rwpResource) Encrypted() bool                { return resource.isEncrypted }
func (resource *rwpResource) Open() (io.ReadCloser, error)   { return resource.file.Open() }
func (resource *rwpResource) CompressBeforeEncryption() bool { return false }
func (resource *rwpResource) CanBeEncrypted() bool           { return true }

func (resource *rwpResource) CopyTo(packageWriter PackageWriter) error {
	wc, err := packageWriter.NewFile(resource.Path(), resource.contentType, resource.file.Method)
	if err != nil {
		return err
	}

	rc, err := resource.file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	_, err = io.Copy(wc, rc)

	rCloseError := rc.Close()
	wCloseError := wc.Close()

	if err != nil {
		return err
	}

	if rCloseError != nil {
		return rCloseError
	}

	return wCloseError
}

// Close closes a NopWriteCloser
func (nc *NopWriteCloser) Close() error {
	return nil
}

// NewFile creates a header for the input file and adds it (with its media type) to the reading order
func (writer *RWPPWriter) NewFile(path string, contentType string, storageMethod uint16) (io.WriteCloser, error) {

	w, err := writer.zipWriter.CreateHeader(&zip.FileHeader{
		Name:   path,
		Method: storageMethod,
	})

	writer.manifest.ReadingOrder = append(writer.manifest.ReadingOrder, rwpm.Link{
		Href: path,
		Type: contentType,
	})

	return &NopWriteCloser{w}, err
}

// MarkAsEncrypted marks a resource as encrypted (with an lcp profile and algorithm), in the manifest
// FIXME: currently only looks into the reading order. Add "resources" and "alternates"
func (writer *RWPPWriter) MarkAsEncrypted(path string, originalSize int64, profile license.EncryptionProfile, algorithm string) {

	for i, resource := range writer.manifest.ReadingOrder {
		if path == resource.Href {
			if resource.Properties == nil {
				writer.manifest.ReadingOrder[i].Properties = new(rwpm.Properties)
			}

			writer.manifest.ReadingOrder[i].Properties.Encrypted = &rwpm.Encrypted{
				Scheme:    "http://readium.org/2014/01/lcp",
				Profile:   profile.String(),
				Algorithm: algorithm,
			}

			break
		}
	}
}

// ManifestLocation is the path if the Readium manifest in a package
const ManifestLocation = "manifest.json"

func (writer *RWPPWriter) writeManifest() error {
	w, err := writer.zipWriter.Create(ManifestLocation)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(w)
	return encoder.Encode(writer.manifest)
}

// Close closes a Readium Package Writer
func (writer *RWPPWriter) Close() error {
	err := writer.writeManifest()
	if err != nil {
		return err
	}

	return writer.zipWriter.Close()
}

// NewRWPPReader creates a new Readium Package reader
func NewRWPPReader(zipReader *zip.Reader) (*RWPPReader, error) {

	// find and parse the manifest
	var manifest rwpm.Publication
	var found bool
	for _, file := range zipReader.File {
		if file.Name == ManifestLocation {
			found = true

			fileReader, err := file.Open()
			if err != nil {
				return nil, err
			}
			decoder := json.NewDecoder(fileReader)

			err = decoder.Decode(&manifest)
			fileReader.Close()
			if err != nil {
				return nil, err
			}
			break
		}
	}

	if !found {
		return nil, errors.New("Could not find manifest")
	}

	return &RWPPReader{zipArchive: zipReader, manifest: manifest}, nil

}

// OpenRWPP opens a Readium Package and returns a zip reader + a manifest
func OpenRWPP(name string) (*RWPPReader, error) {

	zipArchive, err := zip.OpenReader(name)
	if err != nil {
		return nil, err
	}

	return NewRWPPReader(&zipArchive.Reader)
}

// BuildRWPPFromPDF builds a Readium Package (rwpp) which embeds a PDF file
func BuildRWPPFromPDF(title string, inputPath string, outputPath string) error {

	// crate the rwpp
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// copy the content of the pdf input file into the zip output, as 'publication.pdf'
	zipWriter := zip.NewWriter(f)
	writer, err := zipWriter.Create("publication.pdf")
	if err != nil {
		return err
	}
	inputFile, err := os.Open(inputPath)
	if err != nil {
		zipWriter.Close()
		return err
	}
	defer inputFile.Close()

	_, err = io.Copy(writer, inputFile)
	if err != nil {
		zipWriter.Close()
		return err
	}

	// inject a Readium manifest into the zip output
	manifest := `
	{
		"@context": [
			"https://readium.org/webpub-manifest/context.jsonld"
		],
		"metadata": {
			"title": "{{.Title}}"
		},
		"readingOrder": [
			{
				"href": "publication.pdf",
				"type": "application/pdf"
			}
		]
	}
	`

	manifestWriter, err := zipWriter.Create(ManifestLocation)
	if err != nil {
		return err
	}

	tmpl, err := template.New("manifest").Parse(manifest)
	if err != nil {
		zipWriter.Close()
		return err
	}

	err = tmpl.Execute(manifestWriter, struct{ Title string }{title})
	if err != nil {
		zipWriter.Close()
		return err
	}

	return zipWriter.Close()
}
