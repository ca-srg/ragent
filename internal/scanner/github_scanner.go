package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"

	"github.com/ca-srg/ragent/internal/types"
)

type GitHubRepo struct {
	Owner string
	Name  string
}

type GitHubScanner struct {
	repos    []GitHubRepo
	token    string
	tempDirs []string
}

func ParseGitHubRepos(reposStr string) ([]GitHubRepo, error) {
	reposStr = strings.TrimSpace(reposStr)
	if reposStr == "" {
		return nil, fmt.Errorf("github repos string is empty")
	}

	parts := strings.Split(reposStr, ",")
	var repos []GitHubRepo

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		segments := strings.SplitN(part, "/", 3)
		if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
			return nil, fmt.Errorf("invalid GitHub repo format %q: expected \"owner/repo\"", part)
		}

		repos = append(repos, GitHubRepo{
			Owner: segments[0],
			Name:  segments[1],
		})
	}

	if len(repos) == 0 {
		return nil, fmt.Errorf("no valid GitHub repos found in %q", reposStr)
	}

	return repos, nil
}

func NewGitHubScanner(repos []GitHubRepo, token string) *GitHubScanner {
	return &GitHubScanner{
		repos: repos,
		token: token,
	}
}

func (g *GitHubScanner) CloneRepository(ctx context.Context, repo GitHubRepo) (string, error) {
	tmpDir, err := os.MkdirTemp("", fmt.Sprintf("ragent-github-%s-%s-*", repo.Owner, repo.Name))
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	g.tempDirs = append(g.tempDirs, tmpDir)

	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", repo.Owner, repo.Name)

	cloneOpts := &git.CloneOptions{
		URL:   cloneURL,
		Depth: 1,
	}

	if g.token != "" {
		cloneOpts.Auth = &http.BasicAuth{
			Username: "x-access-token",
			Password: g.token,
		}
	}

	log.Printf("Cloning GitHub repository: %s/%s", repo.Owner, repo.Name)

	_, err = git.PlainCloneContext(ctx, tmpDir, false, cloneOpts)
	if err != nil {
		return "", fmt.Errorf("failed to clone %s/%s: %w", repo.Owner, repo.Name, err)
	}

	log.Printf("Successfully cloned %s/%s to %s", repo.Owner, repo.Name, tmpDir)
	return tmpDir, nil
}

func (g *GitHubScanner) ScanRepository(ctx context.Context, repo GitHubRepo, repoDir string) ([]*types.FileInfo, error) {
	var files []*types.FileInfo

	err := filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		if !g.isSupportedFile(path) {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			log.Printf("Warning: failed to get file info for %s: %v", path, err)
			return nil
		}

		relPath, err := filepath.Rel(repoDir, path)
		if err != nil {
			log.Printf("Warning: failed to get relative path for %s: %v", path, err)
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		content, err := os.ReadFile(path)
		if err != nil {
			log.Printf("Warning: failed to read file %s: %v", path, err)
			return nil
		}

		contentStr := string(content)

		fileInfo := &types.FileInfo{
			Path:        fmt.Sprintf("github://%s/%s/%s", repo.Owner, repo.Name, relPath),
			Name:        d.Name(),
			Size:        info.Size(),
			ModTime:     info.ModTime(),
			IsMarkdown:  g.isMarkdownFile(path),
			IsCSV:       g.isCSVFile(path),
			Content:     contentStr,
			ContentHash: ComputeMD5Hash(contentStr),
			SourceType:  "github",
		}

		files = append(files, fileInfo)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan repository %s/%s: %w", repo.Owner, repo.Name, err)
	}

	return files, nil
}

func (g *GitHubScanner) ScanAllRepositories(ctx context.Context) ([]*types.FileInfo, error) {
	var allFiles []*types.FileInfo

	for _, repo := range g.repos {
		repoDir, err := g.CloneRepository(ctx, repo)
		if err != nil {
			log.Printf("Warning: Failed to clone %s/%s: %v (skipping)", repo.Owner, repo.Name, err)
			continue
		}

		files, err := g.ScanRepository(ctx, repo, repoDir)
		if err != nil {
			log.Printf("Warning: Failed to scan %s/%s: %v (skipping)", repo.Owner, repo.Name, err)
			continue
		}

		log.Printf("Found %d supported files in %s/%s", len(files), repo.Owner, repo.Name)
		allFiles = append(allFiles, files...)
	}

	return allFiles, nil
}

func (g *GitHubScanner) Cleanup() {
	for _, dir := range g.tempDirs {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("Warning: failed to remove temp directory %s: %v", dir, err)
		}
	}
	g.tempDirs = nil
}

func (g *GitHubScanner) isMarkdownFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".md" || ext == ".markdown"
}

func (g *GitHubScanner) isCSVFile(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	return ext == ".csv"
}

func (g *GitHubScanner) isSupportedFile(filePath string) bool {
	return g.isMarkdownFile(filePath) || g.isCSVFile(filePath)
}

func IsGitHubPath(path string) bool {
	return strings.HasPrefix(path, "github://")
}
