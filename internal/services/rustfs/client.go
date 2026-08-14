package rustfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"gorm.io/gorm"
)

// Config is persisted in ServiceInstance.ConfigJSON.
type Config struct {
	Port        string `json:"port"`
	ConsolePort string `json:"console_port"`
	AccessKey   string `json:"access_key"`
	SecretKey   string `json:"secret_key"`
	Endpoint    string `json:"endpoint"` // optional override, default http://127.0.0.1:{port}
}

func DefaultConfig() Config {
	return Config{
		Port:        "9000",
		ConsolePort: "9001",
		AccessKey:   "rustfsadmin",
		SecretKey:   "rustfsadmin",
	}
}

func ParseConfig(raw string) Config {
	cfg := DefaultConfig()
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" || strings.Contains(raw, `"user_disabled":true`) {
		return cfg
	}
	_ = json.Unmarshal([]byte(raw), &cfg)
	if cfg.Port == "" {
		cfg.Port = "9000"
	}
	if cfg.ConsolePort == "" {
		cfg.ConsolePort = "9001"
	}
	if cfg.AccessKey == "" {
		cfg.AccessKey = "rustfsadmin"
	}
	if cfg.SecretKey == "" {
		cfg.SecretKey = "rustfsadmin"
	}
	return cfg
}

// LoadConfig reads persisted RustFS config for a server.
func LoadConfig(db *gorm.DB, serverID uint) Config {
	if db == nil {
		return DefaultConfig()
	}
	var inst struct {
		ConfigJSON string
	}
	err := db.Table("service_instances").
		Select("config_json").
		Where("server_id = ? AND service_type = ?", serverID, serviceType).
		Take(&inst).Error
	if err != nil {
		return DefaultConfig()
	}
	return ParseConfig(inst.ConfigJSON)
}

func (c Config) EndpointURL() string {
	if strings.TrimSpace(c.Endpoint) != "" {
		return strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	}
	return "http://127.0.0.1:" + c.Port
}

func (c Config) ConsoleURL() string {
	return "http://127.0.0.1:" + c.ConsolePort
}

func (c Config) JSON() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// Client wraps the AWS S3 SDK pointed at RustFS.
type Client struct {
	s3  *s3.Client
	cfg Config
}

// NewClient builds an S3 client for the given config.
func NewClient(cfg Config) *Client {
	awsCfg := aws.Config{
		Region: "us-east-1",
		Credentials: aws.NewCredentialsCache(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	}
	cli := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(cfg.EndpointURL())
		o.UsePathStyle = true
	})
	return &Client{s3: cli, cfg: cfg}
}

// EnrichFromContainer fills blank credentials from the running rustfs container env.
func EnrichFromContainer(cfg Config) Config {
	out, err := exec.Command("docker", "inspect", "-f",
		`{{range .Config.Env}}{{println .}}{{end}}`, "rustfs").Output()
	if err != nil {
		return cfg
	}
	env := parseEnvLines(string(out))
	if cfg.AccessKey == "" || cfg.AccessKey == "rustfsadmin" {
		if v := firstNonEmpty(env, "RUSTFS_ACCESS_KEY", "RUSTFS_ACCESS_KEY_ID", "RUSTFS_ROOT_USER", "MINIO_ROOT_USER"); v != "" {
			cfg.AccessKey = v
		}
	}
	if cfg.SecretKey == "" || cfg.SecretKey == "rustfsadmin" {
		if v := firstNonEmpty(env, "RUSTFS_SECRET_KEY", "RUSTFS_SECRET_ACCESS_KEY", "RUSTFS_ROOT_PASSWORD", "MINIO_ROOT_PASSWORD"); v != "" {
			cfg.SecretKey = v
		}
	}
	return cfg
}

func parseEnvLines(s string) map[string]string {
	m := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if i := strings.IndexByte(line, '='); i > 0 {
			m[line[:i]] = line[i+1:]
		}
	}
	return m
}

func firstNonEmpty(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	return ""
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.s3.ListBuckets(ctx, &s3.ListBucketsInput{})
	return err
}

// BucketInfo is a bucket plus optional usage stats.
type BucketInfo struct {
	Name         string
	Created      time.Time
	ObjectCount  int64
	TotalBytes   int64
	SizeHuman    string
	CreatedHuman string
}

// ObjectEntry is a file or "folder" (common prefix) in a bucket listing.
type ObjectEntry struct {
	Key       string
	Name      string
	IsDir     bool
	Size      int64
	SizeHuman string
	Mod       string
}

var bucketNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

// ValidateBucketName checks S3-compatible bucket naming rules.
func ValidateBucketName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("bucket name required")
	}
	if !bucketNameRe.MatchString(name) {
		return fmt.Errorf("invalid bucket name (3–63 chars, lowercase letters, digits, . and -)")
	}
	if strings.Contains(name, "..") || strings.HasPrefix(name, "xn--") {
		return fmt.Errorf("invalid bucket name")
	}
	return nil
}

func (c *Client) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	out, err := c.s3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}
	list := make([]BucketInfo, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		info := BucketInfo{Name: aws.ToString(b.Name)}
		if b.CreationDate != nil {
			info.Created = *b.CreationDate
			info.CreatedHuman = b.CreationDate.Local().Format("2006-01-02 15:04")
		}
		count, bytes, _ := c.bucketUsage(ctx, info.Name)
		info.ObjectCount = count
		info.TotalBytes = bytes
		info.SizeHuman = formatBytes(bytes)
		list = append(list, info)
	}
	return list, nil
}

func (c *Client) bucketUsage(ctx context.Context, bucket string) (count, bytes int64, err error) {
	var token *string
	for {
		out, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			ContinuationToken: token,
		})
		if err != nil {
			return count, bytes, err
		}
		for _, obj := range out.Contents {
			count++
			bytes += aws.ToInt64(obj.Size)
		}
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		token = out.NextContinuationToken
	}
	return count, bytes, nil
}

func (c *Client) CreateBucket(ctx context.Context, name string) error {
	if err := ValidateBucketName(name); err != nil {
		return err
	}
	_, err := c.s3.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(name),
	})
	return err
}

func (c *Client) DeleteBucket(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("bucket required")
	}
	_, err := c.s3.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(name),
	})
	return err
}

// ListObjects lists one "directory" level under prefix (delimiter=/).
func (c *Client) ListObjects(ctx context.Context, bucket, prefix string) ([]ObjectEntry, error) {
	bucket = strings.TrimSpace(bucket)
	prefix = normalizePrefix(prefix)
	var token *string
	var entries []ObjectEntry
	seenDirs := map[string]bool{}
	for {
		out, err := c.s3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			Delimiter:         aws.String("/"),
			ContinuationToken: token,
		})
		if err != nil {
			return nil, err
		}
		for _, p := range out.CommonPrefixes {
			full := aws.ToString(p.Prefix)
			if seenDirs[full] {
				continue
			}
			seenDirs[full] = true
			name := strings.TrimSuffix(strings.TrimPrefix(full, prefix), "/")
			if name == "" {
				continue
			}
			entries = append(entries, ObjectEntry{
				Key:   full,
				Name:  name,
				IsDir: true,
			})
		}
		for _, obj := range out.Contents {
			key := aws.ToString(obj.Key)
			if key == "" || key == prefix {
				continue
			}
			name := strings.TrimPrefix(key, prefix)
			if name == "" || strings.Contains(name, "/") {
				continue
			}
			sz := aws.ToInt64(obj.Size)
			mod := ""
			if obj.LastModified != nil {
				mod = obj.LastModified.Local().Format("2006-01-02 15:04")
			}
			entries = append(entries, ObjectEntry{
				Key:       key,
				Name:      name,
				Size:      sz,
				SizeHuman: formatBytes(sz),
				Mod:       mod,
			})
		}
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		token = out.NextContinuationToken
	}
	return entries, nil
}

func normalizePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix == "" {
		return ""
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix
}

func (c *Client) PutObject(ctx context.Context, bucket, key string, body io.Reader, size int64, contentType string) error {
	key = strings.TrimPrefix(strings.TrimSpace(key), "/")
	if bucket == "" || key == "" {
		return fmt.Errorf("bucket and key required")
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   body,
	}
	if size >= 0 {
		input.ContentLength = aws.Int64(size)
	}
	if contentType != "" {
		input.ContentType = aws.String(contentType)
	}
	_, err := c.s3.PutObject(ctx, input)
	return err
}

func (c *Client) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, string, int64, error) {
	key = strings.TrimPrefix(strings.TrimSpace(key), "/")
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", 0, err
	}
	ct := aws.ToString(out.ContentType)
	sz := aws.ToInt64(out.ContentLength)
	return out.Body, ct, sz, nil
}

func (c *Client) DeleteObject(ctx context.Context, bucket, key string) error {
	key = strings.TrimPrefix(strings.TrimSpace(key), "/")
	if bucket == "" || key == "" {
		return fmt.Errorf("bucket and key required")
	}
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	return err
}

// EmptyBucket deletes every object in the bucket.
func (c *Client) EmptyBucket(ctx context.Context, bucket string) error {
	return c.deleteByPrefix(ctx, bucket, "")
}

// DeletePrefix deletes all objects under prefix (including the prefix marker).
func (c *Client) DeletePrefix(ctx context.Context, bucket, prefix string) error {
	prefix = strings.TrimPrefix(strings.TrimSpace(prefix), "/")
	if bucket == "" || prefix == "" {
		return fmt.Errorf("bucket and prefix required")
	}
	return c.deleteByPrefix(ctx, bucket, prefix)
}

func (c *Client) deleteByPrefix(ctx context.Context, bucket, prefix string) error {
	if bucket == "" {
		return fmt.Errorf("bucket required")
	}
	var token *string
	for {
		in := &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			ContinuationToken: token,
		}
		if prefix != "" {
			in.Prefix = aws.String(prefix)
		}
		out, err := c.s3.ListObjectsV2(ctx, in)
		if err != nil {
			return err
		}
		if len(out.Contents) == 0 {
			break
		}
		objs := make([]types.ObjectIdentifier, 0, len(out.Contents))
		for _, o := range out.Contents {
			objs = append(objs, types.ObjectIdentifier{Key: o.Key})
		}
		_, err = c.s3.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{Objects: objs, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return err
		}
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		token = out.NextContinuationToken
	}
	return nil
}

// RenameObject copies src→dest then deletes src (S3 has no rename).
func (c *Client) RenameObject(ctx context.Context, bucket, src, dest string) error {
	src = strings.TrimPrefix(strings.TrimSpace(src), "/")
	dest = strings.TrimPrefix(strings.TrimSpace(dest), "/")
	if bucket == "" || src == "" || dest == "" {
		return fmt.Errorf("bucket, source, and destination required")
	}
	if src == dest {
		return nil
	}
	_, err := c.s3.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(bucket),
		Key:        aws.String(dest),
		CopySource: aws.String(bucket + "/" + encodeCopySourceKey(src)),
	})
	if err != nil {
		return err
	}
	return c.DeleteObject(ctx, bucket, src)
}

// Mkdir creates a zero-byte folder marker at prefix/.
func (c *Client) Mkdir(ctx context.Context, bucket, prefix string) error {
	prefix = strings.TrimPrefix(strings.TrimSpace(prefix), "/")
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return c.PutObject(ctx, bucket, prefix, strings.NewReader(""), 0, "application/x-directory")
}

func formatBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v := float64(n)
	for _, u := range units {
		v /= 1024
		if v < 1024 {
			return fmt.Sprintf("%.1f %s", v, u)
		}
	}
	return fmt.Sprintf("%.1f PB", v/1024)
}

func encodeCopySourceKey(key string) string {
	parts := strings.Split(key, "/")
	for i, p := range parts {
		parts[i] = strings.ReplaceAll(url.PathEscape(p), "+", "%20")
	}
	return strings.Join(parts, "/")
}
