package cloner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/studio-b12/gowebdav"
	"golang.org/x/sync/errgroup"

	"webdav_cloner/internal/config"
)

type Options struct {
	DryRun       bool
	Concurrency  int
	Logger       *log.Logger
	ShowProgress bool
}

func Run(ctx context.Context, cfg *config.Config, opts Options) error {
	if cfg == nil {
		return fmt.Errorf("nil config passed to cloner.Run")
	}

	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}

	for _, job := range cfg.Jobs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := runJob(ctx, job, opts, logger); err != nil {
			return err
		}
	}

	return nil
}

type clientWrapper struct {
	Endpoint config.Endpoint
	Client   *gowebdav.Client
}

func runJob(ctx context.Context, job config.Job, opts Options, logger *log.Logger) error {
	logger.Printf("%s: preparing source %s", job.Name, job.Source.URL)

	sourceClient, err := buildClient(job.Source)
	if err != nil {
		return fmt.Errorf("%s: %w", job.Name, err)
	}

	if err := ensureConnected(ctx, sourceClient, job.Source.Root, job.Name, "source", logger); err != nil {
		return fmt.Errorf("%s: connect source %s: %w", job.Name, job.Source.URL, err)
	}

	targetClients := make([]*clientWrapper, 0, len(job.Targets))
	for _, target := range job.Targets {
		logger.Printf("%s: preparing target %s", job.Name, target.URL)

		c, err := buildClient(target)
		if err != nil {
			return fmt.Errorf("%s: %w", job.Name, err)
		}

		if err := ensureConnected(ctx, c, target.Root, job.Name, "target", logger); err != nil {
			return fmt.Errorf("%s: connect target %s: %w", job.Name, target.URL, err)
		}

		targetClients = append(targetClients, &clientWrapper{
			Endpoint: target,
			Client:   c,
		})
	}

	sourceRoot := job.Source.Root
	if job.Path != "" {
		sourceRoot = path.Join(sourceRoot, job.Path)
	}

	sourceRoot = ensureAbsolute(sourceRoot)

	logger.Printf("%s: cloning %s -> %d target(s)", job.Name, sourceRoot, len(targetClients))

	copyConcurrency := opts.Concurrency
	if job.Concurrency > 0 {
		copyConcurrency = job.Concurrency
	}
	if copyConcurrency <= 0 {
		copyConcurrency = 1
	}

	entries, directories, err := gatherRemoteEntries(ctx, sourceClient, sourceRoot)
	if err != nil {
		return fmt.Errorf("%s: gather entries: %w", job.Name, err)
	}

	for _, relDir := range directories {
		if err := ensureDirectories(ctx, relDir, targetClients, opts.DryRun, logger, job.Name); err != nil {
			return err
		}
	}

	var progress *progressbar.ProgressBar
	if opts.ShowProgress && len(entries) > 0 {
		progress = progressbar.NewOptions(len(entries),
			progressbar.OptionSetDescription(fmt.Sprintf("%s", job.Name)),
			progressbar.OptionShowCount(),
			progressbar.OptionSetPredictTime(false),
			progressbar.OptionClearOnFinish(),
		)
	}

	g, groupCtx := errgroup.WithContext(ctx)
	files := make(chan remoteEntry)

	g.Go(func() error {
		defer close(files)
		for _, entry := range entries {
			select {
			case <-groupCtx.Done():
				return groupCtx.Err()
			case files <- entry:
			}
		}
		return nil
	})

	for i := 0; i < copyConcurrency; i++ {
		g.Go(func() error {
			for {
				select {
				case <-groupCtx.Done():
					return groupCtx.Err()
				case entry, ok := <-files:
					if !ok {
						return nil
					}
					if err := copyFile(groupCtx, sourceClient, targetClients, entry, opts.DryRun, logger, job.Name); err != nil {
						return err
					}
					if progress != nil {
						_ = progress.Add(1)
					}
				}
			}
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("%s: %w", job.Name, err)
	}

	if progress != nil {
		_ = progress.Finish()
	}

	logger.Printf("%s: finished", job.Name)

	return nil
}

type remoteEntry struct {
	sourcePath string
	relative   string
	info       os.FileInfo
}

func buildClient(endpoint config.Endpoint) (*gowebdav.Client, error) {
	if endpoint.URL == "" {
		return nil, errors.New("empty endpoint url")
	}

	client := gowebdav.NewClient(endpoint.URL, endpoint.Username, endpoint.Password)
	client.SetTimeout(60 * time.Second)
	return client, nil
}

func ensureDirectories(ctx context.Context, relative string, targets []*clientWrapper, dryRun bool, logger *log.Logger, jobName string) error {
	for _, target := range targets {
		dstPath := joinRemote(target.Endpoint.Root, relative)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if dryRun {
			logger.Printf("%s: dry-run mkdir %s%s", jobName, target.Endpoint.URL, dstPath)
			continue
		}

		if err := target.Client.MkdirAll(dstPath, 0o755); err != nil {
			return fmt.Errorf("create directory %s on %s: %w", dstPath, target.Endpoint.URL, err)
		}
	}

	return nil
}

func copyFile(ctx context.Context, sourceClient *gowebdav.Client, targets []*clientWrapper, entry remoteEntry, dryRun bool, logger *log.Logger, jobName string) error {
	if dryRun {
		for _, target := range targets {
			dstPath := joinRemote(target.Endpoint.Root, entry.relative)
			logger.Printf("%s: dry-run copy %s -> %s%s", jobName, entry.sourcePath, target.Endpoint.URL, dstPath)
		}
		return nil
	}

	reader, err := sourceClient.ReadStream(entry.sourcePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", entry.sourcePath, err)
	}

	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		return fmt.Errorf("read %s: %w", entry.sourcePath, readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", entry.sourcePath, closeErr)
	}

	var once sync.Once
	for _, target := range targets {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		dstPath := joinRemote(target.Endpoint.Root, entry.relative)

		skipped, err := shouldSkip(target.Client, dstPath, entry.info)
		if err != nil {
			return fmt.Errorf("inspect %s%s: %w", target.Endpoint.URL, dstPath, err)
		}
		if skipped {
			logger.Printf("%s: skipped %s -> %s%s (up-to-date)", jobName, entry.sourcePath, target.Endpoint.URL, dstPath)
			continue
		}

		if err := target.Client.MkdirAll(path.Dir(dstPath), 0o755); err != nil {
			return fmt.Errorf("prepare parent for %s%s: %w", target.Endpoint.URL, dstPath, err)
		}

		buf := bytes.NewReader(data)
		if err := target.Client.WriteStream(dstPath, buf, entry.info.Mode()); err != nil {
			return fmt.Errorf("write to %s%s: %w", target.Endpoint.URL, dstPath, err)
		}

		once.Do(func() {
			logger.Printf("%s: copied %s (%d bytes)", jobName, entry.sourcePath, len(data))
		})

		// WebDAV servers may adjust modification time automatically.
	}

	return nil
}

func shouldSkip(client *gowebdav.Client, dstPath string, sourceInfo os.FileInfo) (bool, error) {
	info, err := client.Stat(dstPath)
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, err
	}

	if info == nil {
		return false, nil
	}

	if info.Size() != sourceInfo.Size() {
		return false, nil
	}

	srcMod := sourceInfo.ModTime().Truncate(time.Second)
	dstMod := info.ModTime().Truncate(time.Second)
	if srcMod.IsZero() || dstMod.Before(srcMod) {
		return false, nil
	}

	return true, nil
}

func walkRemote(ctx context.Context, client *gowebdav.Client, root string, fn func(string, os.FileInfo) error) error {
	info, err := client.Stat(root)
	if err != nil {
		return fmt.Errorf("stat %s: %w", root, err)
	}

	if !info.IsDir() {
		return fn(root, info)
	}

	return walkRemoteDir(ctx, client, root, fn)
}

func walkRemoteDir(ctx context.Context, client *gowebdav.Client, root string, fn func(string, os.FileInfo) error) error {
	entries, err := client.ReadDir(root)
	if err != nil {
		return fmt.Errorf("list %s: %w", root, err)
	}

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		current := path.Join(root, entry.Name())

		if err := fn(current, entry); err != nil {
			return err
		}

		if entry.IsDir() {
			if err := walkRemoteDir(ctx, client, current, fn); err != nil {
				return err
			}
		}
	}

	return nil
}

func joinRemote(root, relative string) string {
	relative = strings.TrimLeft(relative, "/")
	if relative == "" {
		return root
	}
	return path.Join(root, relative)
}

func relativePath(root, full string) string {
	if root == "" {
		return strings.TrimLeft(full, "/")
	}

	trimmed := strings.TrimPrefix(full, root)
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" && full == root {
		return path.Base(full)
	}
	return trimmed
}

func ensureAbsolute(p string) string {
	if p == "" {
		return "/"
	}
	if strings.HasPrefix(p, "/") {
		return path.Clean(p)
	}
	return "/" + path.Clean(p)
}

func ensureConnected(ctx context.Context, client *gowebdav.Client, root, jobName, role string, logger *log.Logger) error {
	if err := client.Connect(); err != nil {
		if !canBypassConnectError(err) {
			return err
		}

		// Some servers close OPTIONS requests unexpectedly. Probe with Stat as a fallback.
		if _, statErr := client.Stat(root); statErr == nil {
			logger.Printf("%s: %s connection fallback succeeded via STAT despite: %v", jobName, role, err)
			return nil
		}

		if _, dirErr := client.ReadDir(root); dirErr == nil {
			logger.Printf("%s: %s connection fallback succeeded via PROPFIND despite: %v", jobName, role, err)
			return nil
		}

		return err
	}
	return nil
}

func canBypassConnectError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if errors.Is(urlErr.Err, io.EOF) || errors.Is(urlErr.Err, io.ErrUnexpectedEOF) {
			return true
		}
		return strings.Contains(strings.ToLower(urlErr.Error()), "eof")
	}

	return strings.Contains(strings.ToLower(err.Error()), "eof")
}

func gatherRemoteEntries(ctx context.Context, client *gowebdav.Client, root string) ([]remoteEntry, []string, error) {
	var (
		files   []remoteEntry
		dirSet  = make(map[string]struct{})
		dirList []string
	)

	err := walkRemote(ctx, client, root, func(p string, info os.FileInfo) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		rel := relativePath(root, p)
		if info.IsDir() {
			if rel != "" {
				dirSet[rel] = struct{}{}
			}
			return nil
		}

		files = append(files, remoteEntry{
			sourcePath: p,
			relative:   rel,
			info:       info,
		})
		return nil
	})

	if err != nil {
		return nil, nil, err
	}

	for dir := range dirSet {
		dirList = append(dirList, dir)
	}

	sort.Strings(dirList)

	return files, dirList, nil
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}

	var statusErr *gowebdav.StatusError
	if errors.As(err, &statusErr) && statusErr.Status == http.StatusNotFound {
		return true
	}

	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		if isNotFoundError(pathErr.Err) {
			return true
		}
		if se, ok := pathErr.Err.(gowebdav.StatusError); ok && se.Status == http.StatusNotFound {
			return true
		}
		if se, ok := pathErr.Err.(*gowebdav.StatusError); ok && se.Status == http.StatusNotFound {
			return true
		}
	}

	return false
}
