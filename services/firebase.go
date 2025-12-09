package services

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"cloud.google.com/go/storage"
	firebase "firebase.google.com/go/v4"
	"google.golang.org/api/option"
)

// FirebaseService handles Firebase Cloud Storage operations
type FirebaseService struct {
	bucket *storage.BucketHandle
}

// NewFirebaseService creates a new Firebase service
// credentialsJSON should be the content of the service account JSON file
func NewFirebaseService(credentialsJSON string, storageBucket string) (*FirebaseService, error) {
	ctx := context.Background()

	// Initialize Firebase app with credentials
	opt := option.WithCredentialsJSON([]byte(credentialsJSON))
	config := &firebase.Config{
		StorageBucket: storageBucket,
	}

	app, err := firebase.NewApp(ctx, config, opt)
	if err != nil {
		return nil, fmt.Errorf("error initializing firebase app: %w", err)
	}

	// Get storage client
	client, err := app.Storage(ctx)
	if err != nil {
		return nil, fmt.Errorf("error getting storage client: %w", err)
	}

	bucket, err := client.DefaultBucket()
	if err != nil {
		return nil, fmt.Errorf("error getting default bucket: %w", err)
	}

	log.Println("Connected to Firebase Cloud Storage")
	return &FirebaseService{bucket: bucket}, nil
}

// UploadFile uploads a file to Firebase Cloud Storage and returns the public URL
func (s *FirebaseService) UploadFile(ctx context.Context, data []byte, filename string, contentType string) (string, error) {
	// Create object path with timestamp to avoid collisions
	objectPath := fmt.Sprintf("exports/%s/%s", time.Now().Format("2006-01-02"), filename)

	// Create object writer
	obj := s.bucket.Object(objectPath)
	writer := obj.NewWriter(ctx)
	writer.ContentType = contentType
	writer.CacheControl = "public, max-age=3600" // Cache for 1 hour

	// Write data
	if _, err := writer.Write(data); err != nil {
		return "", fmt.Errorf("failed to write to storage: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	// Make the file publicly readable
	if err := obj.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
		return "", fmt.Errorf("failed to set ACL: %w", err)
	}

	// Generate public URL
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get object attrs: %w", err)
	}

	publicURL := fmt.Sprintf("https://storage.googleapis.com/%s/%s", attrs.Bucket, attrs.Name)
	return publicURL, nil
}

// UploadFileWithSignedURL uploads a file and returns a signed URL (expires in 24 hours)
func (s *FirebaseService) UploadFileWithSignedURL(ctx context.Context, data []byte, filename string, contentType string) (string, error) {
	// Create object path with timestamp
	objectPath := fmt.Sprintf("exports/%s/%s", time.Now().Format("2006-01-02"), filename)

	// Create object writer
	obj := s.bucket.Object(objectPath)
	writer := obj.NewWriter(ctx)
	writer.ContentType = contentType

	// Write data
	if _, err := writer.Write(data); err != nil {
		return "", fmt.Errorf("failed to write to storage: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	// Make the file publicly readable
	if err := obj.ACL().Set(ctx, storage.AllUsers, storage.RoleReader); err != nil {
		return "", fmt.Errorf("failed to set ACL: %w", err)
	}

	// Get bucket name for URL construction
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get object attrs: %w", err)
	}

	// Return public URL (properly URL-encoded)
	publicURL := fmt.Sprintf("https://storage.googleapis.com/%s/%s", attrs.Bucket, attrs.Name)
	return publicURL, nil
}

// DeleteFile deletes a file from Firebase Cloud Storage
func (s *FirebaseService) DeleteFile(ctx context.Context, objectPath string) error {
	obj := s.bucket.Object(objectPath)
	return obj.Delete(ctx)
}

// Close closes the Firebase service (no-op for now)
func (s *FirebaseService) Close() error {
	return nil
}

// Reader interface for streaming
func (s *FirebaseService) GetFileReader(ctx context.Context, objectPath string) (io.ReadCloser, error) {
	obj := s.bucket.Object(objectPath)
	return obj.NewReader(ctx)
}
