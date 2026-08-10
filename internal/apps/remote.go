package apps

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultCatalogBase is CapRover's official one-click app repository (v4).
// List:  GET {base}/list
// App:   GET {base}/apps/{name}  (JSON; same schema as the .yml sources)
// Logos: GET {base}/logos/{name}.png
const DefaultCatalogBase = "https://oneclickapps.caprover.com/v4"

type catalogList struct {
	OneClickApps []catalogEntry `json:"oneClickApps"`
}

type catalogEntry struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
	IsOfficial  bool   `json:"isOfficial"`
	LogoURL     string `json:"logoUrl"`
}

// CatalogClient fetches CapRover-compatible one-click apps over HTTP.
type CatalogClient struct {
	Base   string
	Client *http.Client
}

func defaultCatalogClient() *CatalogClient {
	return &CatalogClient{
		Base: DefaultCatalogBase,
		Client: &http.Client{
			Timeout: 25 * time.Second,
		},
	}
}

func (c *CatalogClient) base() string {
	if c == nil || strings.TrimSpace(c.Base) == "" {
		return DefaultCatalogBase
	}
	return strings.TrimRight(c.Base, "/")
}

func (c *CatalogClient) http() *http.Client {
	if c != nil && c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 25 * time.Second}
}

// ListSummaries loads the remote catalog index.
func (c *CatalogClient) ListSummaries() ([]Summary, error) {
	body, err := c.get(c.base() + "/list")
	if err != nil {
		return nil, err
	}
	var list catalogList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("parsing catalog list: %w", err)
	}
	out := make([]Summary, 0, len(list.OneClickApps))
	for _, e := range list.OneClickApps {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		display := e.DisplayName
		if display == "" {
			display = name
		}
		out = append(out, Summary{
			ID:          name,
			DisplayName: display,
			Description: e.Description,
			IsOfficial:  e.IsOfficial,
			LogoURL:     c.logoURL(e.LogoURL, name),
		})
	}
	return out, nil
}

// FetchAppJSON downloads one app definition (CapRover dist JSON).
func (c *CatalogClient) FetchAppJSON(id string) ([]byte, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") {
		return nil, fmt.Errorf("invalid app id")
	}
	return c.get(c.base() + "/apps/" + url.PathEscape(id))
}

func (c *CatalogClient) logoURL(logo, name string) string {
	logo = strings.TrimSpace(logo)
	if logo == "" {
		logo = name + ".png"
	}
	if strings.HasPrefix(logo, "http://") || strings.HasPrefix(logo, "https://") {
		return logo
	}
	return c.base() + "/logos/" + strings.TrimLeft(logo, "/")
}

func (c *CatalogClient) get(rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "xmanager-oneclick/1.0")
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog %s: HTTP %d", rawURL, res.StatusCode)
	}
	return body, nil
}
