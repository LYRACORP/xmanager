package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

func deliverS3(cfg DestConfig, localPath, filename string) error {
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return fmt.Errorf("s3 bucket required")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return fmt.Errorf("s3 access_key and secret_key required")
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("s3 open local file: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("s3 stat local file: %w", err)
	}

	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}
	awsCfg := aws.Config{
		Region: region,
		Credentials: aws.NewCredentialsCache(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	var client *s3.Client
	if endpoint != "" {
		client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(strings.TrimRight(endpoint, "/"))
			o.UsePathStyle = true
		})
	} else {
		client = s3.NewFromConfig(awsCfg)
	}

	key := s3ObjectKey(cfg.Prefix, filename)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          f,
		ContentLength: aws.Int64(info.Size()),
	})
	if err != nil {
		return fmt.Errorf("s3 upload: %w", err)
	}
	return nil
}

func s3ObjectKey(prefix, filename string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	filename = strings.TrimPrefix(strings.TrimSpace(filename), "/")
	if prefix == "" {
		return filename
	}
	return prefix + "/" + filename
}

func deliverGDrive(cfg DestConfig, localPath, filename string) error {
	credJSON := strings.TrimSpace(cfg.GDriveCredJSON)
	if credJSON == "" {
		return fmt.Errorf("google drive service account JSON required")
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("gdrive open local file: %w", err)
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	creds, err := google.CredentialsFromJSON(ctx, []byte(credJSON), drive.DriveFileScope)
	if err != nil {
		return fmt.Errorf("gdrive credentials: %w", err)
	}

	svc, err := drive.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return fmt.Errorf("gdrive client: %w", err)
	}

	folderID := strings.TrimSpace(cfg.GDriveFolderID)
	if folderID == "" {
		folderID = "root"
	}

	driveFile := &drive.File{
		Name:    filepath.Base(filename),
		Parents: []string{folderID},
	}
	_, err = svc.Files.Create(driveFile).Media(f).Do()
	if err != nil {
		return fmt.Errorf("gdrive upload: %w", err)
	}
	return nil
}
