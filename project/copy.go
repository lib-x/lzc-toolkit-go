package project

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

func (service *Service) CopyTo(ctx context.Context, request CopyRequest) (CopyResult, error) {
	appID := strings.TrimSpace(request.AppID)
	serviceName := normalizedService(request.Service)
	if err := service.validate(ctx, "project.copy", appID); err != nil {
		return CopyResult{}, err
	}
	if !validIdentifier(serviceName) {
		return CopyResult{}, projectError(lpkgo.CodeInvalidArgument, "project.copy", errors.New("invalid service name"))
	}
	rawSourcePath := strings.TrimSpace(request.SourcePath)
	if rawSourcePath == "" {
		return CopyResult{}, projectError(lpkgo.CodeInvalidArgument, "project.copy", errors.New("source path is required"))
	}
	sourcePath, err := filepath.Abs(rawSourcePath)
	if err != nil {
		return CopyResult{}, projectError(lpkgo.CodeInvalidArgument, "project.copy", errors.New("invalid source path"))
	}
	if _, err := os.Lstat(sourcePath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return CopyResult{}, projectError(lpkgo.CodeNotFound, "project.copy", errors.New("source path not found"))
		}
		return CopyResult{}, projectError(lpkgo.CodeCommandFailed, "project.copy", errors.New("source path is unavailable"))
	}
	destination := strings.TrimSpace(request.Destination)
	if !safeCopyDestination(destination) {
		return CopyResult{}, projectError(lpkgo.CodeInvalidArgument, "project.copy", errors.New("invalid destination path"))
	}
	containerID, err := service.ensureServiceRunning(ctx, appID, serviceName)
	if err != nil {
		return CopyResult{}, err
	}

	archiveReader, archiveWriter := io.Pipe()
	archiveDone := make(chan error, 1)
	go func() {
		writeErr := writeSourceTar(ctx, archiveWriter, sourcePath)
		_ = archiveWriter.CloseWithError(writeErr)
		archiveDone <- writeErr
	}()
	commandResult, commandErr := service.Docker(ctx, DockerRequest{
		Args:  []string{"cp", "-", containerID + ":" + destination},
		Stdin: archiveReader, Stdout: request.Stdout, Stderr: request.Stderr,
	})
	_ = archiveReader.CloseWithError(commandErr)
	archiveErr := <-archiveDone
	if commandErr != nil {
		return CopyResult{}, commandErr
	}
	if archiveErr != nil {
		if ctx.Err() != nil {
			return CopyResult{}, projectError(lpkgo.CodeCancelled, "project.copy", ctx.Err())
		}
		return CopyResult{}, projectError(lpkgo.CodeCommandFailed, "project.copy", errors.New("creating source archive failed"))
	}
	return CopyResult{ContainerID: containerID, SourcePath: sourcePath, Destination: destination, Command: commandResult}, nil
}

func writeSourceTar(ctx context.Context, destination io.Writer, sourcePath string) error {
	writer := tar.NewWriter(destination)
	parent := filepath.Dir(sourcePath)
	err := filepath.WalkDir(sourcePath, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return errors.New("unsupported source file type")
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(current)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(parent, current)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		header.Uid = 0
		header.Gid = 0
		header.Uname = ""
		header.Gname = ""
		header.AccessTime = time.Time{}
		header.ChangeTime = time.Time{}
		if err := writer.WriteHeader(header); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		file, err := os.Open(current)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(writer, contextReader{ctx: ctx, reader: file})
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

func safeCopyDestination(value string) bool {
	if value == "" || strings.ContainsAny(value, ":\r\n\x00") || path.Clean(value) != value {
		return false
	}
	for _, component := range strings.Split(value, "/") {
		if component == ".." {
			return false
		}
	}
	return true
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}
