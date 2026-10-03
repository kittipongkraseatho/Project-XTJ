// Package cloudinary provides a single integration point for the Cloudinary
// upload + delivery API. Per spec §17, the SDK import lives ONLY in this
// package so it doesn't pollute the rest of the codebase.
package cloudinary

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/cloudinary/cloudinary-go/v2"
	"github.com/cloudinary/cloudinary-go/v2/api/uploader"
)

// Asset is the normalized upload result stored in trade_images.
type Asset struct {
	PublicID    string `json:"public_id"`
	AssetID     string `json:"asset_id"`
	ResourceType string `json:"resource_type"`
	Format      string `json:"format"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Bytes       int64  `json:"bytes"`
	SecureURL   string `json:"secure_url"`
	Folder      string `json:"folder"`
	Version     int64  `json:"version"`
}

// Service is the public interface for image operations.
//
// Implemented by CloudinaryService (this file) and StubService (no-op).
type Service interface {
	UploadImage(ctx context.Context, folder string, filename string, src io.Reader) (*Asset, error)
	DeleteImage(ctx context.Context, publicID string) error
	GetThumbnailURL(publicID string) string
}

// ── Real implementation (wraps SDK) ────────────────────────────────────

// CloudinaryService wraps the cloudinary-go SDK.
type CloudinaryService struct {
	cld *cloudinary.Cloudinary
}

func New(cloudName, apiKey, apiSecret string) (*CloudinaryService, error) {
	cld, err := cloudinary.NewFromURL(
		fmt.Sprintf("cloudinary://%s:%s@%s", apiKey, apiSecret, cloudName),
	)
	if err != nil {
		return nil, fmt.Errorf("init cloudinary: %w", err)
	}
	return &CloudinaryService{cld: cld}, nil
}

func (s *CloudinaryService) UploadImage(ctx context.Context, folder, filename string, src io.Reader) (*Asset, error) {
	if s == nil || s.cld == nil {
		return nil, fmt.Errorf("cloudinary service not initialized")
	}
	publicID := strings.TrimSuffix(filename, path.Ext(filename))
	fullFolder := strings.Trim(folder, "/")
	fullID := fullFolder + "/" + publicID

	uploadParams := uploader.UploadParams{
		Folder:          fullFolder,
		PublicID:        publicID, // combined with Folder by SDK
		Overwrite:       boolPtr(true),
		UniqueFilename:  boolPtr(false),
	}
	resp, err := s.cld.Upload.Upload(ctx, src, uploadParams)
	if err != nil {
		return nil, fmt.Errorf("cloudinary upload: %w", err)
	}
	return &Asset{
		PublicID:     fullID,
		AssetID:      resp.AssetID,
		ResourceType: resp.ResourceType,
		Format:       resp.Format,
		Width:        resp.Width,
		Height:       resp.Height,
		Bytes:        int64(resp.Bytes),
		SecureURL:    resp.SecureURL,
		Folder:       fullFolder,
		Version:      int64(resp.Version),
	}, nil
}

func (s *CloudinaryService) DeleteImage(ctx context.Context, publicID string) error {
	if s == nil || s.cld == nil {
		return fmt.Errorf("cloudinary service not initialized")
	}
	_, err := s.cld.Upload.Destroy(ctx, uploader.DestroyParams{PublicID: publicID})
	if err != nil {
		return fmt.Errorf("cloudinary delete: %w", err)
	}
	return nil
}

// GetThumbnailURL builds a thumbnail URL with c_thumb transform (150x150 fit).
// Phase 1: simple — Phase 4 can add named transformations.
func (s *CloudinaryService) GetThumbnailURL(publicID string) string {
	// Build via Cloudinary's URL builder: /image/upload/c_thumb,h_150,w_150/{publicID}
	return "image/upload/c_thumb,h_150,w_150/" + publicID
}

// ── Stub implementation (no env vars set) ──────────────────────────────

// StubService returns "not implemented" errors. Used when Cloudinary env is missing.
type StubService struct{}

func (StubService) UploadImage(ctx context.Context, folder, filename string, src io.Reader) (*Asset, error) {
	return nil, fmt.Errorf("cloudinary not configured")
}
func (StubService) DeleteImage(ctx context.Context, publicID string) error {
	return fmt.Errorf("cloudinary not configured")
}
func (StubService) GetThumbnailURL(publicID string) string { return "" }

// ── tiny pointer helpers ──────────────────────────────────────────────

func boolPtr(b bool) *bool { return &b }