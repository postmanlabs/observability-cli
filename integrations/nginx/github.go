package nginx

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/akitasoftware/akita-cli/printer"
)

// The Github API asset type, with most fields missing
type GithubAsset struct {
	Name string `json:"name"`
	ID   int    `json:"id"`
}

// The Github API release type, with most fields missing
type GithubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []GithubAsset `json:"assets"`
}

// Return a list of all the assets available in the latest release
func GetLatestReleaseAssets() ([]GithubAsset, error) {
	response, err := http.Get("https://api.github.com/repos/akitasoftware/akita-nginx-module/releases/latest")
	if err != nil {
		printer.Debugf("Error performing GET request: %v", err)
		return nil, err
	}
	defer response.Body.Close()

	printer.Debugf("Status: %q\n", response.Status)
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("Response code %d from Github", response.StatusCode)
	}

	decoder := json.NewDecoder(response.Body)
	var release GithubRelease
	err = decoder.Decode(&release)
	if err != nil {
		printer.Debugf("JSON decode error: %v\n", err)
		return nil, err
	}

	return release.Assets, nil
}

// Download a specific asset (the prebuilt module) to a temporary file
func DownloadReleaseAsset(id int, filename string) error {
	download, err := os.Create(filename)
	if err != nil {
		printer.Errorf("Can't create destination file: %v\n", err)
		return err
	}

	// Need to set a header to get the binary rather than JSON
	client := &http.Client{}
	url := fmt.Sprintf("https://api.github.com/repos/akitasoftware/akita-nginx-module/releases/assets/%d", id)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		printer.Errorf("Failed to create download request: %v\n", err)
		return err
	}

	req.Header.Add("Accept", "application/octet-stream")
	response, err := client.Do(req)
	if err != nil {
		printer.Errorf("HTTP download failure: %v\n", err)
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		printer.Errorf("Module download has status %q\n", response.Status)
		return fmt.Errorf("Response code %d from Github", response.StatusCode)
	}

	_, err = io.Copy(download, response.Body)
	if err != nil {
		printer.Errorf("HTTP download failure: %v\n", err)
		return err
	}

	return nil
}
