package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/disintegration/imaging"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"github.com/usememos/memos/api/auth"
	"github.com/usememos/memos/common/log"
	"github.com/usememos/memos/common/util"
	"github.com/usememos/memos/plugin/storage/s3"
	"github.com/usememos/memos/store"
	"go.uber.org/zap"
)

type Resource struct {
	ID int32 `json:"id"`

	// Standard fields
	CreatorID int32 `json:"creatorId"`
	CreatedTs int64 `json:"createdTs"`
	UpdatedTs int64 `json:"updatedTs"`

	// Domain specific fields
	Filename     string `json:"filename"`
	Blob         []byte `json:"-"`
	InternalPath string `json:"-"`
	ExternalLink string `json:"externalLink"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`

	// Related fields
	LinkedMemoAmount int `json:"linkedMemoAmount"`
}

type CreateResourceRequest struct {
	Filename        string `json:"filename"`
	InternalPath    string `json:"internalPath"`
	ExternalLink    string `json:"externalLink"`
	Type            string `json:"type"`
	DownloadToLocal bool   `json:"downloadToLocal"`
}

type FindResourceRequest struct {
	ID        *int32  `json:"id"`
	CreatorID *int32  `json:"creatorId"`
	Filename  *string `json:"filename"`
}

type UpdateResourceRequest struct {
	Filename *string `json:"filename"`
}

const (
	// The upload memory buffer is 32 MiB.
	// It should be kept low, so RAM usage doesn't get out of control.
	// This is unrelated to maximum upload size limit, which is now set through system setting.
	maxUploadBufferSizeBytes = 32 << 20
	MebiByte                 = 1024 * 1024

	// thumbnailImagePath is the directory to store image thumbnails.
	thumbnailImagePath = ".thumbnail_cache"
)

var fileKeyPattern = regexp.MustCompile(`\{[a-z]{1,9}\}`)

func (s *APIV1Service) registerResourceRoutes(g *echo.Group) {
	g.POST("/resource", func(c echo.Context) error {
		ctx := c.Request().Context()
		userID, ok := c.Get(auth.UserIDContextKey).(int32)
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "Missing user in session")
		}

		request := &CreateResourceRequest{}
		if err := json.NewDecoder(c.Request().Body).Decode(request); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Malformatted post resource request").SetInternal(err)
		}

		create := &store.Resource{
			CreatorID:    userID,
			Filename:     request.Filename,
			ExternalLink: request.ExternalLink,
			Type:         request.Type,
		}
		if request.ExternalLink != "" {
			// Only allow those external links scheme with http/https
			linkURL, err := url.Parse(request.ExternalLink)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "Invalid external link").SetInternal(err)
			}
			if linkURL.Scheme != "http" && linkURL.Scheme != "https" {
				return echo.NewHTTPError(http.StatusBadRequest, "Invalid external link scheme")
			}

			if request.DownloadToLocal {
				resp, err := http.Get(linkURL.String())
				if err != nil {
					return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Failed to request %s", request.ExternalLink))
				}
				defer resp.Body.Close()

				blob, err := io.ReadAll(resp.Body)
				if err != nil {
					return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Failed to read %s", request.ExternalLink))
				}

				mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
				if err != nil {
					return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Failed to read mime from %s", request.ExternalLink))
				}
				create.Type = mediaType

				filename := path.Base(linkURL.Path)
				if path.Ext(filename) == "" {
					extensions, _ := mime.ExtensionsByType(mediaType)
					if len(extensions) > 0 {
						filename += extensions[0]
					}
				}
				create.Filename = filename
				create.ExternalLink = ""
				create.Size = int64(len(blob))

				err = SaveResourceBlob(ctx, s.Store, create, bytes.NewReader(blob))
				if err != nil {
					return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save resource").SetInternal(err)
				}
			}
		}

		resource, err := s.Store.CreateResource(ctx, create)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create resource").SetInternal(err)
		}
		if err := s.createResourceCreateActivity(ctx, resource); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create activity").SetInternal(err)
		}
		return c.JSON(http.StatusOK, convertResourceFromStore(resource))
	})

	g.POST("/resource/blob", func(c echo.Context) error {
		ctx := c.Request().Context()
		userID, ok := c.Get(auth.UserIDContextKey).(int32)
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "Missing user in session")
		}

		// This is the backend default max upload size limit.
		maxUploadSetting := s.Store.GetSystemSettingValueWithDefault(&ctx, SystemSettingMaxUploadSizeMiBName.String(), "32")
		var settingMaxUploadSizeBytes int
		if settingMaxUploadSizeMiB, err := strconv.Atoi(maxUploadSetting); err == nil {
			settingMaxUploadSizeBytes = settingMaxUploadSizeMiB * MebiByte
		} else {
			log.Warn("Failed to parse max upload size", zap.Error(err))
			settingMaxUploadSizeBytes = 0
		}

		file, err := c.FormFile("file")
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get uploading file").SetInternal(err)
		}
		if file == nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Upload file not found").SetInternal(err)
		}

		if file.Size > int64(settingMaxUploadSizeBytes) {
			message := fmt.Sprintf("File size exceeds allowed limit of %d MiB", settingMaxUploadSizeBytes/MebiByte)
			return echo.NewHTTPError(http.StatusBadRequest, message).SetInternal(err)
		}
		if err := c.Request().ParseMultipartForm(maxUploadBufferSizeBytes); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Failed to parse upload data").SetInternal(err)
		}

		sourceFile, err := file.Open()
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to open file").SetInternal(err)
		}
		defer sourceFile.Close()

		create := &store.Resource{
			CreatorID: userID,
			Filename:  file.Filename,
			Type:      file.Header.Get("Content-Type"),
			Size:      file.Size,
		}
		err = SaveResourceBlob(ctx, s.Store, create, sourceFile)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save resource").SetInternal(err)
		}

		resource, err := s.Store.CreateResource(ctx, create)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create resource").SetInternal(err)
		}
		if err := s.createResourceCreateActivity(ctx, resource); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create activity").SetInternal(err)
		}
		return c.JSON(http.StatusOK, convertResourceFromStore(resource))
	})

	g.GET("/resource", func(c echo.Context) error {
		ctx := c.Request().Context()
		userID, ok := c.Get(auth.UserIDContextKey).(int32)
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "Missing user in session")
		}
		find := &store.FindResource{
			CreatorID: &userID,
		}
		if limit, err := strconv.Atoi(c.QueryParam("limit")); err == nil {
			find.Limit = &limit
		}
		if offset, err := strconv.Atoi(c.QueryParam("offset")); err == nil {
			find.Offset = &offset
		}

		list, err := s.Store.ListResources(ctx, find)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to fetch resource list").SetInternal(err)
		}
		resourceMessageList := []*Resource{}
		for _, resource := range list {
			resourceMessageList = append(resourceMessageList, convertResourceFromStore(resource))
		}
		return c.JSON(http.StatusOK, resourceMessageList)
	})

	g.PATCH("/resource/:resourceId", func(c echo.Context) error {
		ctx := c.Request().Context()
		userID, ok := c.Get(auth.UserIDContextKey).(int32)
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "Missing user in session")
		}

		resourceID, err := util.ConvertStringToInt32(c.Param("resourceId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("ID is not a number: %s", c.Param("resourceId"))).SetInternal(err)
		}

		resource, err := s.Store.GetResource(ctx, &store.FindResource{
			ID: &resourceID,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find resource").SetInternal(err)
		}
		if resource == nil {
			return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("Resource not found: %d", resourceID))
		}
		if resource.CreatorID != userID {
			return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized")
		}

		request := &UpdateResourceRequest{}
		if err := json.NewDecoder(c.Request().Body).Decode(request); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Malformatted patch resource request").SetInternal(err)
		}

		currentTs := time.Now().Unix()
		update := &store.UpdateResource{
			ID:        resourceID,
			UpdatedTs: &currentTs,
		}
		if request.Filename != nil && *request.Filename != "" {
			update.Filename = request.Filename
		}

		resource, err = s.Store.UpdateResource(ctx, update)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to patch resource").SetInternal(err)
		}
		return c.JSON(http.StatusOK, convertResourceFromStore(resource))
	})

	g.DELETE("/resource/:resourceId", func(c echo.Context) error {
		ctx := c.Request().Context()
		userID, ok := c.Get(auth.UserIDContextKey).(int32)
		if !ok {
			return echo.NewHTTPError(http.StatusUnauthorized, "Missing user in session")
		}

		resourceID, err := util.ConvertStringToInt32(c.Param("resourceId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("ID is not a number: %s", c.Param("resourceId"))).SetInternal(err)
		}

		resource, err := s.Store.GetResource(ctx, &store.FindResource{
			ID:        &resourceID,
			CreatorID: &userID,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to find resource").SetInternal(err)
		}
		if resource == nil {
			return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("Resource not found: %d", resourceID))
		}

		if resource.InternalPath != "" {
			if err := os.Remove(resource.InternalPath); err != nil {
				log.Warn(fmt.Sprintf("failed to delete local file with path %s", resource.InternalPath), zap.Error(err))
			}
		}

		ext := filepath.Ext(resource.Filename)
		thumbnailPath := filepath.Join(s.Profile.Data, thumbnailImagePath, fmt.Sprintf("%d%s", resource.ID, ext))
		if err := os.Remove(thumbnailPath); err != nil {
			log.Warn(fmt.Sprintf("failed to delete local thumbnail with path %s", thumbnailPath), zap.Error(err))
		}

		if err := s.Store.DeleteResource(ctx, &store.DeleteResource{
			ID: resourceID,
		}); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete resource").SetInternal(err)
		}
		return c.JSON(http.StatusOK, true)
	})
}

func (s *APIV1Service) registerResourcePublicRoutes(g *echo.Group) {
	f := func(c echo.Context) error {
		ctx := c.Request().Context()
		resourceID, err := util.ConvertStringToInt32(c.Param("resourceId"))
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("ID is not a number: %s", c.Param("resourceId"))).SetInternal(err)
		}

		resourceVisibility, err := checkResourceVisibility(ctx, s.Store, resourceID)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Failed to get resource visibility").SetInternal(err)
		}

		// Protected resource require a logined user
		userID, ok := c.Get(auth.UserIDContextKey).(int32)
		if resourceVisibility == store.Protected && (!ok || userID <= 0) {
			return echo.NewHTTPError(http.StatusUnauthorized, "Resource visibility not match").SetInternal(err)
		}

		resource, err := s.Store.GetResource(ctx, &store.FindResource{
			ID:      &resourceID,
			GetBlob: true,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to find resource by ID: %v", resourceID)).SetInternal(err)
		}
		if resource == nil {
			return echo.NewHTTPError(http.StatusNotFound, fmt.Sprintf("Resource not found: %d", resourceID))
		}

		// Private resource require logined user is the creator
		if resourceVisibility == store.Private && (!ok || userID != resource.CreatorID) {
			return echo.NewHTTPError(http.StatusUnauthorized, "Resource visibility not match").SetInternal(err)
		}

		blob := resource.Blob
		if resource.InternalPath != "" {
			resourcePath := resource.InternalPath
			src, err := os.Open(resourcePath)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to open the local resource: %s", resourcePath)).SetInternal(err)
			}
			defer src.Close()
			blob, err = io.ReadAll(src)
			if err != nil {
				return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to read the local resource: %s", resourcePath)).SetInternal(err)
			}
		}

		if c.QueryParam("thumbnail") == "1" && util.HasPrefixes(resource.Type, "image/png", "image/jpeg") {
			ext := filepath.Ext(resource.Filename)
			thumbnailPath := filepath.Join(s.Profile.Data, thumbnailImagePath, fmt.Sprintf("%d%s", resource.ID, ext))
			thumbnailBlob, err := getOrGenerateThumbnailImage(blob, thumbnailPath)
			if err != nil {
				log.Warn(fmt.Sprintf("failed to get or generate local thumbnail with path %s", thumbnailPath), zap.Error(err))
			} else {
				blob = thumbnailBlob
			}
		}

		c.Response().Writer.Header().Set(echo.HeaderCacheControl, "max-age=31536000, immutable")
		c.Response().Writer.Header().Set(echo.HeaderContentSecurityPolicy, "default-src 'self'")
		resourceType := strings.ToLower(resource.Type)
		if strings.HasPrefix(resourceType, "text") {
			resourceType = echo.MIMETextPlainCharsetUTF8
		} else if strings.HasPrefix(resourceType, "video") || strings.HasPrefix(resourceType, "audio") {
			http.ServeContent(c.Response(), c.Request(), resource.Filename, time.Unix(resource.UpdatedTs, 0), bytes.NewReader(blob))
			return nil
		}
		return c.Stream(http.StatusOK, resourceType, bytes.NewReader(blob))
	}

	g.GET("/r/:resourceId", f)
	g.GET("/r/:resourceId/*", f)
}

func (s *APIV1Service) createResourceCreateActivity(ctx context.Context, resource *store.Resource) error {
	payload := ActivityResourceCreatePayload{
		Filename: resource.Filename,
		Type:     resource.Type,
		Size:     resource.Size,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "failed to marshal activity payload")
	}
	activity, err := s.Store.CreateActivity(ctx, &store.Activity{
		CreatorID: resource.CreatorID,
		Type:      ActivityResourceCreate.String(),
		Level:     ActivityInfo.String(),
		Payload:   string(payloadBytes),
	})
	if err != nil || activity == nil {
		return errors.Wrap(err, "failed to create activity")
	}
	return err
}

func replacePathTemplate(path, filename string) string {
	t := time.Now()
	path = fileKeyPattern.ReplaceAllStringFunc(path, func(s string) string {
		switch s {
		case "{filename}":
			return filename
		case "{timestamp}":
			return fmt.Sprintf("%d", t.Unix())
		case "{year}":
			return fmt.Sprintf("%d", t.Year())
		case "{month}":
			return fmt.Sprintf("%02d", t.Month())
		case "{day}":
			return fmt.Sprintf("%02d", t.Day())
		case "{hour}":
			return fmt.Sprintf("%02d", t.Hour())
		case "{minute}":
			return fmt.Sprintf("%02d", t.Minute())
		case "{second}":
			return fmt.Sprintf("%02d", t.Second())
		}
		return s
	})
	return path
}

var availableGeneratorAmount int32 = 32

func getOrGenerateThumbnailImage(srcBlob []byte, dstPath string) ([]byte, error) {
	if _, err := os.Stat(dstPath); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Wrap(err, "failed to check thumbnail image stat")
		}

		if atomic.LoadInt32(&availableGeneratorAmount) <= 0 {
			return nil, errors.New("not enough available generator amount")
		}
		atomic.AddInt32(&availableGeneratorAmount, -1)
		defer func() {
			atomic.AddInt32(&availableGeneratorAmount, 1)
		}()

		reader := bytes.NewReader(srcBlob)
		src, err := imaging.Decode(reader, imaging.AutoOrientation(true))
		if err != nil {
			return nil, errors.Wrap(err, "failed to decode thumbnail image")
		}
		thumbnailImage := imaging.Resize(src, 512, 0, imaging.Lanczos)

		dstDir := path.Dir(dstPath)
		if err := os.MkdirAll(dstDir, os.ModePerm); err != nil {
			return nil, errors.Wrap(err, "failed to create thumbnail dir")
		}

		if err := imaging.Save(thumbnailImage, dstPath); err != nil {
			return nil, errors.Wrap(err, "failed to resize thumbnail image")
		}
	}

	dstFile, err := os.Open(dstPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open the local resource")
	}
	defer dstFile.Close()
	dstBlob, err := io.ReadAll(dstFile)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the local resource")
	}
	return dstBlob, nil
}

func checkResourceVisibility(ctx context.Context, s *store.Store, resourceID int32) (store.Visibility, error) {
	memoResources, err := s.ListMemoResources(ctx, &store.FindMemoResource{
		ResourceID: &resourceID,
	})
	if err != nil {
		return store.Private, err
	}

	// If resource is belongs to no memo, it'll always PRIVATE.
	if len(memoResources) == 0 {
		return store.Private, nil
	}

	memoIDs := make([]int32, 0, len(memoResources))
	for _, memoResource := range memoResources {
		memoIDs = append(memoIDs, memoResource.MemoID)
	}
	visibilityList, err := s.FindMemosVisibilityList(ctx, memoIDs)
	if err != nil {
		return store.Private, err
	}

	var isProtected bool
	for _, visibility := range visibilityList {
		// If any memo is PUBLIC, resource should be PUBLIC too.
		if visibility == store.Public {
			return store.Public, nil
		}

		if visibility == store.Protected {
			isProtected = true
		}
	}

	if isProtected {
		return store.Protected, nil
	}

	return store.Private, nil
}

func convertResourceFromStore(resource *store.Resource) *Resource {
	return &Resource{
		ID:               resource.ID,
		CreatorID:        resource.CreatorID,
		CreatedTs:        resource.CreatedTs,
		UpdatedTs:        resource.UpdatedTs,
		Filename:         resource.Filename,
		Blob:             resource.Blob,
		InternalPath:     resource.InternalPath,
		ExternalLink:     resource.ExternalLink,
		Type:             resource.Type,
		Size:             resource.Size,
		LinkedMemoAmount: resource.LinkedMemoAmount,
	}
}

// SaveResourceBlob save the blob of resource based on the storage config
//
// Depend on the storage config, some fields of *store.ResourceCreate will be changed:
// 1. *DatabaseStorage*: `create.Blob`.
// 2. *LocalStorage*: `create.InternalPath`.
// 3. Others( external service): `create.ExternalLink`.
func SaveResourceBlob(ctx context.Context, s *store.Store, create *store.Resource, r io.Reader) error {
	systemSettingStorageServiceID, err := s.GetSystemSetting(ctx, &store.FindSystemSetting{Name: SystemSettingStorageServiceIDName.String()})
	if err != nil {
		return fmt.Errorf("Failed to find SystemSettingStorageServiceIDName: %s", err)
	}

	storageServiceID := DatabaseStorage
	if systemSettingStorageServiceID != nil {
		err = json.Unmarshal([]byte(systemSettingStorageServiceID.Value), &storageServiceID)
		if err != nil {
			return fmt.Errorf("Failed to unmarshal storage service id: %s", err)
		}
	}

	// `DatabaseStorage` means store blob into database
	if storageServiceID == DatabaseStorage {
		fileBytes, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("Failed to read file: %s", err)
		}
		create.Blob = fileBytes
		return nil
	}

	// `LocalStorage` means save blob into local disk
	if storageServiceID == LocalStorage {
		systemSettingLocalStoragePath, err := s.GetSystemSetting(ctx, &store.FindSystemSetting{Name: SystemSettingLocalStoragePathName.String()})
		if err != nil {
			return fmt.Errorf("Failed to find SystemSettingLocalStoragePathName: %s", err)
		}
		localStoragePath := "assets/{filename}"
		if systemSettingLocalStoragePath != nil && systemSettingLocalStoragePath.Value != "" {
			err = json.Unmarshal([]byte(systemSettingLocalStoragePath.Value), &localStoragePath)
			if err != nil {
				return fmt.Errorf("Failed to unmarshal SystemSettingLocalStoragePathName: %s", err)
			}
		}
		filePath := filepath.FromSlash(localStoragePath)
		if !strings.Contains(filePath, "{filename}") {
			filePath = filepath.Join(filePath, "{filename}")
		}
		filePath = filepath.Join(s.Profile.Data, replacePathTemplate(filePath, create.Filename))

		dir := filepath.Dir(filePath)
		if err = os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("Failed to create directory: %s", err)
		}
		dst, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("Failed to create file: %s", err)
		}
		defer dst.Close()
		_, err = io.Copy(dst, r)
		if err != nil {
			return fmt.Errorf("Failed to copy file: %s", err)
		}

		create.InternalPath = filePath
		return nil
	}

	// Others: store blob into external service, such as S3
	storage, err := s.GetStorage(ctx, &store.FindStorage{ID: &storageServiceID})
	if err != nil {
		return fmt.Errorf("Failed to find StorageServiceID: %s", err)
	}
	if storage == nil {
		return fmt.Errorf("Storage %d not found", storageServiceID)
	}
	storageMessage, err := ConvertStorageFromStore(storage)
	if err != nil {
		return fmt.Errorf("Failed to ConvertStorageFromStore: %s", err)
	}

	if storageMessage.Type != StorageS3 {
		return fmt.Errorf("Unsupported storage type: %s", storageMessage.Type)
	}

	s3Config := storageMessage.Config.S3Config
	s3Client, err := s3.NewClient(ctx, &s3.Config{
		AccessKey: s3Config.AccessKey,
		SecretKey: s3Config.SecretKey,
		EndPoint:  s3Config.EndPoint,
		Region:    s3Config.Region,
		Bucket:    s3Config.Bucket,
		URLPrefix: s3Config.URLPrefix,
		URLSuffix: s3Config.URLSuffix,
	})
	if err != nil {
		return fmt.Errorf("Failed to create s3 client: %s", err)
	}

	filePath := s3Config.Path
	if !strings.Contains(filePath, "{filename}") {
		filePath = filepath.Join(filePath, "{filename}")
	}
	filePath = replacePathTemplate(filePath, create.Filename)

	link, err := s3Client.UploadFile(ctx, filePath, create.Type, r)
	if err != nil {
		return fmt.Errorf("Failed to upload via s3 client: %s", err)
	}

	create.ExternalLink = link
	return nil
}
