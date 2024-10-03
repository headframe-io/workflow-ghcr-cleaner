// GHCR untagged cleaner
//
// Deletes all truly untagged GHCR containers in a repository. Tags that are not depended on by other tags
// will be deleted. This scenario can happen when using multi-arch packages.

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	PER_PAGE        = 100
	DOCKER_ENDPOINT = "ghcr.io"
)

var (
	GITHUB_API_URL string
	GITHUB_TOKEN   string
	DRY_RUN        bool
)

func main() {
	// Command-line arguments
	token := flag.String("token", "", "GitHub Personal access token with delete:packages permissions")
	repoOwner := flag.String("repo-owner", "", "The repository owner name")
	repoName := flag.String("repo-name", "", "Delete containers only from this repository")
	packageName := flag.String("package-name", "", "Delete only package name")
	ownerType := flag.String("owner-type", "org", "Owner type (org or user)")
	dryRun := flag.Bool("dry-run", false, "Run the script without making any changes.")
	deleteUntagged := flag.Bool("delete-untagged", true, "Delete package versions that have no tags and are not a dependency of other tags.")
	keepAtMost := flag.Int("keep-at-most", 5, "Keep at most the given amount of image versions. Only applies to tagged image versions.")
	filterTagsArg := flag.String("filter-tags", "", "Comma-separated list of tags to filter for when using --keep-at-most. Accepts tags as Unix shell-style wildcards.")
	skipTagsArg := flag.String("skip-tags", "", "Comma-separated list of tags to ignore when using --keep-at-most. Accepts tags as Unix shell-style wildcards.")

	flag.Parse()

	if *token == "" {
		fmt.Println("Error: --token is required")
		os.Exit(1)
	}
	if *repoOwner == "" {
		fmt.Println("Error: --repo-owner is required")
		os.Exit(1)
	}

	// Process repo-name if needed
	if strings.Contains(*repoName, "/") {
		parts := strings.SplitN(*repoName, "/", 2)
		if strings.ToLower(parts[0]) != strings.ToLower(*repoOwner) {
			fmt.Printf("Mismatch in repository: %s and owner: %s\n", *repoName, *repoOwner)
			os.Exit(1)
		}
		*repoName = parts[1]
	}
	// Strip leading/trailing slashes from package-name
	*packageName = strings.Trim(*packageName, "/")

	// Process filter-tags and skip-tags
	filterTags := processArgList(*filterTagsArg)
	skipTags := processArgList(*skipTagsArg)

	GITHUB_API_URL = os.Getenv("GITHUB_API_URL")
	if GITHUB_API_URL == "" {
		GITHUB_API_URL = "https://api.github.com"
	}
	GITHUB_TOKEN = *token
	DRY_RUN = *dryRun

	unwantedVersions, err := run(*ownerType, *repoOwner, *repoName, *packageName, *keepAtMost, filterTags, skipTags, *deleteUntagged)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
	exitCode := bulkDelete(unwantedVersions)
	os.Exit(exitCode)
}

func processArgList(arg string) []string {
	var result []string
	if arg != "" {
		arg = strings.ReplaceAll(arg, "\n", ",")
		parts := strings.Split(arg, ",")
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
	}
	return result
}

func requestGithubAPI(relativeURL string, method string, params url.Values, body io.Reader) (*http.Response, error) {
	fullURL, err := url.Parse(GITHUB_API_URL)
	if err != nil {
		return nil, err
	}
	rel, err := url.Parse(relativeURL)
	if err != nil {
		return nil, err
	}
	fullURL = fullURL.ResolveReference(rel)
	if params != nil {
		fullURL.RawQuery = params.Encode()
	}

	req, err := http.NewRequest(method, fullURL.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+GITHUB_TOKEN)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	return resp, err
}

func parseNextLink(linkHeader string) string {
	links := strings.Split(linkHeader, ",")
	for _, link := range links {
		parts := strings.Split(strings.TrimSpace(link), ";")
		if len(parts) < 2 {
			continue
		}
		urlPart := strings.Trim(parts[0], "<>")
		relPart := strings.TrimSpace(parts[1])
		if relPart == `rel="next"` {
			return urlPart
		}
	}
	return ""
}

type Version struct {
	ID     int64
	Digest string
	Date   time.Time
	Tags   []string
	URL    string
	Pkg    *Package
}

func (v *Version) matchTags(patterns []string) bool {
	for _, pattern := range patterns {
		for _, tag := range v.Tags {
			matched, err := filepath.Match(pattern, tag)
			if err != nil {
				continue
			}
			if matched {
				return true
			}
		}
	}
	return false
}

type Manifest struct {
	Manifests []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
		Size      int    `json:"size"`
		Platform  struct {
			Architecture string `json:"architecture"`
			OS           string `json:"os"`
			Variant      string `json:"variant,omitempty"`
		} `json:"platform,omitempty"`
	} `json:"manifests"`
}

func (v *Version) getDeps() ([]string, error) {
	if len(v.Tags) > 0 {
		repository := v.Pkg.Owner + "/" + v.Pkg.Name
		manifestJSON, err := getManifest(repository, v.Digest)
		if err != nil {
			return nil, err
		}
		var manifest Manifest
		if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
			return nil, err
		}
		var deps []string
		for _, m := range manifest.Manifests {
			deps = append(deps, m.Digest)
		}
		return deps, nil
	} else {
		return nil, nil
	}
}

func (v *Version) delete() error {
	fmt.Printf("Deleting %s: ", v.Digest)
	if DRY_RUN {
		fmt.Println("Dry Run")
		return nil
	}

	resp, err := requestGithubAPI(v.URL, "DELETE", nil, nil)
	if err != nil {
		fmt.Println(err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 {
		fmt.Println("OK")
		return nil
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Error: %s, %s\n", resp.Status, string(body))
		return fmt.Errorf("Failed to delete: %s", resp.Status)
	}
}

type Package struct {
	Name       string
	VersionURL string
	Owner      string
	OwnerType  string
}

type PackageData struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Repository struct {
		Name string `json:"name"`
	} `json:"repository"`
}

type VersionData struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	UpdatedAt string `json:"updated_at"`
	Metadata  struct {
		Container struct {
			Tags []string `json:"tags"`
		} `json:"container"`
	} `json:"metadata"`
}

func (p *Package) getVersions() ([]Version, error) {
	var versions []Version

	params := url.Values{}
	params.Set("per_page", strconv.Itoa(PER_PAGE))

	urlPath := p.VersionURL

	for {
		resp, err := requestGithubAPI(urlPath, "GET", params, nil)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("GitHub API error: %s, %s", resp.Status, string(body))
		}

		var versionDatas []VersionData
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&versionDatas); err != nil {
			return nil, err
		}

		for _, vd := range versionDatas {
			date, err := time.Parse(time.RFC3339, vd.UpdatedAt)
			if err != nil {
				return nil, err
			}
			version := Version{
				ID:     vd.ID,
				Digest: vd.Name,
				Date:   date,
				Tags:   vd.Metadata.Container.Tags,
				URL:    vd.URL,
				Pkg:    p,
			}
			versions = append(versions, version)
		}

		// Check for next page
		if linkHeader := resp.Header.Get("Link"); linkHeader != "" {
			nextURL := parseNextLink(linkHeader)
			if nextURL != "" {
				u, err := url.Parse(nextURL)
				if err != nil {
					return nil, err
				}
				urlPath = u.Path
				params = u.Query()
				continue
			}
		}
		break
	}
	return versions, nil
}
func getAllPackages(ownerType, owner, repoName, packageName string) ([]Package, error) {
    var packages []Package

    path := fmt.Sprintf("/%ss/%s/packages", ownerType, owner)
    params := url.Values{}
    params.Set("per_page", strconv.Itoa(PER_PAGE))
    params.Set("package_type", "container") // Added this line

    for {
        resp, err := requestGithubAPI(path, "GET", params, nil)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("GitHub API error: %s, %s", resp.Status, string(body))
		}

		var packageDatas []PackageData
		decoder := json.NewDecoder(resp.Body)
		if err := decoder.Decode(&packageDatas); err != nil {
			return nil, err
		}

		for _, pd := range packageDatas {
			if repoName != "" && strings.ToLower(pd.Repository.Name) != strings.ToLower(repoName) {
				continue
			}
			if packageName != "" && pd.Name != packageName {
				continue
			}
			pkg := Package{
				Name:       pd.Name,
				VersionURL: pd.URL + "/versions",
				Owner:      owner,
				OwnerType:  ownerType,
			}
			packages = append(packages, pkg)
		}

		// Check for next page
		if linkHeader := resp.Header.Get("Link"); linkHeader != "" {
			nextURL := parseNextLink(linkHeader)
			if nextURL != "" {
				u, err := url.Parse(nextURL)
				if err != nil {
					return nil, err
				}
				path = u.Path
				params = u.Query()
				continue
			}
		}
		break
	}
	return packages, nil
}

func getManifest(repository, reference string) ([]byte, error) {
	url := fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", repository, reference)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json,application/vnd.docker.distribution.manifest.list.v2+json")
	req.Header.Set("Authorization", "Bearer "+GITHUB_TOKEN)

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Failed to get manifest: %s, %s", resp.Status, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func run(ownerType, repoOwner, repoName, packageName string, keepAtMost int, filterTags, skipTags []string, deleteUntagged bool) ([]*Version, error) {
	var unwantedVersions []*Version

	packages, err := getAllPackages(ownerType, repoOwner, repoName, packageName)
	if err != nil {
		return nil, err
	}

	for _, pkg := range packages {
		fmt.Printf("Processing package: %s... ", pkg.Name)
		versions, err := pkg.getVersions()
		if err != nil {
			fmt.Printf("Error getting versions: %v\n", err)
			continue
		}
		var tagged []*Version
		var untagged []*Version
		var unwanted []*Version

		for i := range versions {
			version := &versions[i]
			if len(version.Tags) > 0 {
				tagged = append(tagged, version)
			} else {
				untagged = append(untagged, version)
			}
		}

		tagCount := len(tagged)

		// Keep the most recent image versions of the given amount
		if keepAtMost > 0 {
			var sortableList []*Version
			for _, version := range tagged {
				// Skip if we have skip tags, and we find a match
				if len(skipTags) > 0 && version.matchTags(skipTags) {
					continue
				}
				// Skip if we have filter tags, and we do not find a match
				if len(filterTags) > 0 && !version.matchTags(filterTags) {
					continue
				}
				sortableList = append(sortableList, version)
			}
			// Remove all old versions after the most recent count is hit
			sort.Slice(sortableList, func(i, j int) bool {
				return sortableList[i].Date.After(sortableList[j].Date)
			})
			for count, version := range sortableList {
				if count >= keepAtMost {
					unwanted = append(unwanted, version)
				}
			}
			// Remove unwanted versions from tagged
			var newTagged []*Version
			for _, version := range tagged {
				isUnwanted := false
				for _, uv := range unwanted {
					if uv.ID == version.ID {
						isUnwanted = true
						break
					}
				}
				if !isUnwanted {
					newTagged = append(newTagged, version)
				}
			}
			tagged = newTagged
		}

		// Delete untagged versions
		if deleteUntagged {
			tagDependencies := make(map[string]bool)
			// Build set of dependencies
			for _, version := range tagged {
				deps, err := version.getDeps()
				if err != nil {
					fmt.Printf("Error getting dependencies for version %s: %v\n", version.Digest, err)
					continue
				}
				for _, dep := range deps {
					tagDependencies[dep] = true
				}
			}
			// Collect list of all untagged versions that are not dependencies of other versions
			for _, version := range untagged {
				if !tagDependencies[version.Digest] {
					unwanted = append(unwanted, version)
				}
			}
		}
		fmt.Printf("(total=%d, tagged=%d, untagged=%d, unwanted=%d)\n", len(versions), tagCount, len(untagged), len(unwanted))
		unwantedVersions = append(unwantedVersions, unwanted...)
	}

	return unwantedVersions, nil
}

func bulkDelete(deleteList []*Version) int {
	successCount := 0
	errorCount := 0
	for _, version := range deleteList {
		err := version.delete()
		if err != nil {
			errorCount++
		} else {
			successCount++
		}
	}
	fmt.Println()
	fmt.Printf("%d Deletions\n", successCount)
	fmt.Printf("%d Errors\n", errorCount)
	if errorCount > 0 {
		return 1
	}
	return 0
}
