package handler

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rizxfrog/oh-my-api/internal/api/service"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

// uploadImagesFromCanonicalRequest scans the canonical request for image blocks,
// uploads them to Lingma CDN, and returns the CDN URLs.
func (s *Server) uploadImagesFromCanonicalRequest(ctx context.Context, credential proxy.CredentialSnapshot, req proxy.CanonicalRequest) ([]string, error) {
	var imageURLs []string
	for _, turn := range req.Turns {
		for _, block := range turn.Blocks {
			if block.Type != proxy.CanonicalBlockImage && block.Type != proxy.CanonicalBlockDocument {
				continue
			}
			var src proxy.ImageSource
			if err := json.Unmarshal(block.Data, &src); err != nil {
				return nil, fmt.Errorf("invalid image source: %w", err)
			}

			var imageURI string
			switch src.Type {
			case "base64":
				imageURI = "data:" + src.MediaType + ";base64," + src.Data
			case "url":
				imageURI = src.Data
			default:
				return nil, fmt.Errorf("unsupported image source type %q", src.Type)
			}

			cdnURL, err := s.Deps.Uploader.UploadImage(ctx, credential, imageURI)
			if err != nil {
				return nil, fmt.Errorf("uploading image: %w", err)
			}
			imageURLs = append(imageURLs, cdnURL)
		}
	}
	return imageURLs, nil
}

func (s *Server) uploadImagesWithAdapter(ctx context.Context, adapter proxy.RegionAdapter, account proxy.AccountSnapshot, req proxy.CanonicalRequest) ([]string, error) {
	var imageURLs []string
	for _, turn := range req.Turns {
		for _, block := range turn.Blocks {
			if block.Type != proxy.CanonicalBlockImage && block.Type != proxy.CanonicalBlockDocument {
				continue
			}
			var src proxy.ImageSource
			if err := json.Unmarshal(block.Data, &src); err != nil {
				return nil, fmt.Errorf("invalid image source: %w", err)
			}

			var imageURI string
			switch src.Type {
			case "base64":
				imageURI = "data:" + src.MediaType + ";base64," + src.Data
			case "url":
				imageURI = src.Data
			default:
				return nil, fmt.Errorf("unsupported image source type %q", src.Type)
			}

			cdnURL, err := adapter.UploadImage(ctx, account, imageURI)
			if err != nil {
				return nil, fmt.Errorf("uploading image: %w", err)
			}
			imageURLs = append(imageURLs, cdnURL)
		}
	}
	return imageURLs, nil
}

func attachAccountRoutingMetadata(request *proxy.CanonicalRequest, account proxy.AccountSnapshot) {
	request.Metadata = service.CloneMetadataMap(request.Metadata)
	if request.Metadata == nil {
		request.Metadata = map[string]any{}
	}
	request.Metadata["account_id"] = account.ID
	if account.Label != "" {
		request.Metadata["account_label"] = account.Label
	}
	request.Metadata["account_region"] = string(account.Region)
}
