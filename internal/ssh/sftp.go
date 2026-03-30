package ssh

import (
	"fmt"
	"io"
	"os"

	"github.com/pkg/sftp"
)

type SFTPClient struct {
	client     *Client
	sftpClient *sftp.Client
}

func NewSFTPClient(client *Client) (*SFTPClient, error) {
	sc, err := sftp.NewClient(client.conn)
	if err != nil {
		return nil, fmt.Errorf("creating SFTP client: %w", err)
	}
	return &SFTPClient{client: client, sftpClient: sc}, nil
}

func (s *SFTPClient) Close() error {
	return s.sftpClient.Close()
}

func (s *SFTPClient) ReadFile(path string) ([]byte, error) {
	f, err := s.sftpClient.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening remote file %s: %w", path, err)
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (s *SFTPClient) WriteFile(path string, data []byte, perm os.FileMode) error {
	f, err := s.sftpClient.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("creating remote file %s: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing remote file %s: %w", path, err)
	}

	return s.sftpClient.Chmod(path, perm)
}

func (s *SFTPClient) Stat(path string) (os.FileInfo, error) {
	return s.sftpClient.Stat(path)
}

func (s *SFTPClient) ListDir(path string) ([]os.FileInfo, error) {
	return s.sftpClient.ReadDir(path)
}

func (s *SFTPClient) MkdirAll(path string) error {
	return s.sftpClient.MkdirAll(path)
}

func (s *SFTPClient) Remove(path string) error {
	return s.sftpClient.Remove(path)
}
