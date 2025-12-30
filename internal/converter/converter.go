package converter

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BatmanBruc/bat-bot-convetor/internal/formats"
	"github.com/go-telegram/bot"
)

type Converter interface {
	Convert(ctx context.Context, bot *bot.Bot, fileID string, originalExt, targetExt string, originalFileName string, options map[string]interface{}) (resultPath string, resultFileName string, err error)
}

type DefaultConverter struct {
	tempDir string
}

func NewDefaultConverter() *DefaultConverter {
	tempDir := filepath.Join(os.TempDir(), "bot_converter")
	_ = os.MkdirAll(tempDir, 0755)
	return &DefaultConverter{
		tempDir: tempDir,
	}
}

func (c *DefaultConverter) Convert(ctx context.Context, botClient *bot.Bot, fileID string, originalExt, targetExt string, originalFileName string, options map[string]interface{}) (string, string, error) {
	originalExt = strings.ToLower(strings.TrimPrefix(originalExt, "."))
	targetExt = strings.ToLower(strings.TrimPrefix(targetExt, "."))

	if !formats.FormatExists(targetExt) {
		return "", "", fmt.Errorf("неподдерживаемый целевой формат: %s", targetExt)
	}

	fileInfo, err := botClient.GetFile(ctx, &bot.GetFileParams{
		FileID: fileID,
	})
	if err != nil {
		return "", "", fmt.Errorf("ошибка получения файла: %v", err)
	}

	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", botClient.Token(), fileInfo.FilePath)

	nonce := time.Now().UnixNano()
	originalPath := filepath.Join(c.tempDir, fmt.Sprintf("%s_%d_original.%s", fileID, nonce, originalExt))
	resultPath := filepath.Join(c.tempDir, fmt.Sprintf("%s_%d_result.%s", fileID, nonce, targetExt))
	resultFileName := buildResultFileName(originalFileName, targetExt)

	if err := c.downloadFile(ctx, fileURL, originalPath); err != nil {
		return "", "", fmt.Errorf("ошибка загрузки файла: %v", err)
	}
	defer func() { _ = os.Remove(originalPath) }()

	if err := c.convertFile(ctx, originalPath, resultPath, originalExt, targetExt, options); err != nil {
		_ = os.Remove(originalPath)
		_ = os.Remove(resultPath)
		return "", "", fmt.Errorf("ошибка конвертации: %v", err)
	}

	info, err := os.Stat(resultPath)
	if os.IsNotExist(err) {
		_ = os.Remove(originalPath)
		return "", "", fmt.Errorf("файл результата не был создан: %s", resultPath)
	}
	if err != nil {
		return "", "", fmt.Errorf("не удалось прочитать файл результата: %v", err)
	}
	if info.Size() == 0 {
		_ = os.Remove(resultPath)
		return "", "", fmt.Errorf("файл результата пустой: %s", resultPath)
	}

	return resultPath, resultFileName, nil
}

func (c *DefaultConverter) downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{
		Timeout: 30 * time.Minute,
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ошибка загрузки файла: статус %d", resp.StatusCode)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		_ = os.Remove(destPath)
		return err
	}
	return nil
}

func (c *DefaultConverter) convertFile(ctx context.Context, inputPath, outputPath string, originalExt, targetExt string, options map[string]interface{}) error {
	originalExt = strings.ToLower(originalExt)
	targetExt = strings.ToLower(targetExt)

	if originalExt == targetExt {
		if c.isImageFormat(originalExt) && c.isImageFormat(targetExt) && hasImageOptions(options) {
			return c.convertImage(ctx, inputPath, outputPath, originalExt, targetExt, options)
		}
		return c.copyFile(inputPath, outputPath)
	}

	if c.isImageFormat(originalExt) && c.isImageFormat(targetExt) {
		return c.convertImage(ctx, inputPath, outputPath, originalExt, targetExt, options)
	}

	if c.isAudioFormat(originalExt) && c.isAudioFormat(targetExt) {
		return c.convertAudio(ctx, inputPath, outputPath, originalExt, targetExt)
	}

	if c.isVideoFormat(originalExt) && c.isAudioFormat(targetExt) {
		return c.convertVideoToAudio(ctx, inputPath, outputPath)
	}

	if c.isVideoFormat(originalExt) && targetExt == "gif" {
		return c.convertVideoToGif(ctx, inputPath, outputPath, options)
	}

	if c.isVideoFormat(originalExt) && c.isVideoFormat(targetExt) {
		return c.convertVideo(ctx, inputPath, outputPath, originalExt, targetExt, options)
	}

	if c.isEbookFormat(originalExt) && c.isEbookFormat(targetExt) {
		return c.convertEbook(ctx, inputPath, outputPath, originalExt, targetExt)
	}

	if c.isEbookFormat(originalExt) && targetExt == "pdf" {
		return c.convertEbook(ctx, inputPath, outputPath, originalExt, targetExt)
	}

	if c.isDocumentFormat(originalExt) && c.isDocumentFormat(targetExt) {
		return c.convertDocument(ctx, inputPath, outputPath, originalExt, targetExt)
	}

	return fmt.Errorf("конвертация из %s в %s не поддерживается", originalExt, targetExt)
}

func (c *DefaultConverter) copyFile(src, dst string) error {
	if filepath.Clean(src) == filepath.Clean(dst) {
		return nil
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func (c *DefaultConverter) convertImage(ctx context.Context, inputPath, outputPath string, originalExt, targetExt string, options map[string]interface{}) error {
	_ = originalExt

	if !c.hasCommand("magick") && !c.hasCommand("convert") {
		return fmt.Errorf("ImageMagick не установлен")
	}

	if !c.isImageFormat(targetExt) {
		return fmt.Errorf("целевой формат %s не является форматом изображения", targetExt)
	}

	cmdName := "magick"
	if !c.hasCommand("magick") {
		cmdName = "convert"
	}

	args := []string{inputPath}
	if max, ok := optInt(options, "img_max"); ok && max > 0 {
		args = append(args, "-resize", fmt.Sprintf("%dx%d>", max, max))
	}
	if w, ok := optInt(options, "img_w"); ok && w > 0 {
		if h, ok := optInt(options, "img_h"); ok && h > 0 {
			bg := "white"
			if bgs, ok := optString(options, "img_bg"); ok {
				bg = bgs
			}
			args = append(args,
				"-resize", fmt.Sprintf("%dx%d", w, h),
				"-background", bg,
				"-gravity", "center",
				"-extent", fmt.Sprintf("%dx%d", w, h),
			)
		}
	}
	if q, ok := optInt(options, "img_quality"); ok && q > 0 {
		if q > 95 {
			q = 95
		}
		if q < 10 {
			q = 10
		}
		args = append(args, "-quality", fmt.Sprintf("%d", q))
	}
	args = append(args, outputPath)

	cmd := exec.CommandContext(ctx, cmdName, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ошибка ImageMagick: %v, вывод: %s", err, string(output))
	}

	return nil
}

func hasImageOptions(options map[string]interface{}) bool {
	if options == nil {
		return false
	}
	if _, ok := options["img_op"]; ok {
		return true
	}
	if _, ok := options["img_quality"]; ok {
		return true
	}
	if _, ok := options["img_max"]; ok {
		return true
	}
	if _, ok := options["img_w"]; ok {
		return true
	}
	if _, ok := options["img_h"]; ok {
		return true
	}
	return false
}

func optString(m map[string]interface{}, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return "", false
		}
		return s, true
	default:
		return "", false
	}
}

func optInt(m map[string]interface{}, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case int:
		return t, true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func (c *DefaultConverter) convertAudio(ctx context.Context, inputPath, outputPath string, originalExt, targetExt string) error {
	_ = originalExt
	if !c.hasCommand("ffmpeg") {
		return fmt.Errorf("ffmpeg не установлен")
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", "-i", inputPath, "-y", outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ошибка ffmpeg: %v, вывод: %s", err, string(output))
	}

	return nil
}

func (c *DefaultConverter) convertVideo(ctx context.Context, inputPath, outputPath string, originalExt, targetExt string, options map[string]interface{}) error {
	_ = originalExt
	if !c.hasCommand("ffmpeg") {
		return fmt.Errorf("ffmpeg не установлен")
	}

	args := []string{"-i", inputPath}
	vf := ""
	if w, ok := optInt(options, "vid_w"); ok && w > 0 {
		if h, ok := optInt(options, "vid_h"); ok && h > 0 {
			vf = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1", w, h, w, h)
		}
	}
	if h, ok := optInt(options, "vid_height"); ok && h > 0 {
		if vf == "" {
			vf = fmt.Sprintf("scale=-2:%d", h)
		}
	}
	if crf, ok := optInt(options, "vid_crf"); ok && crf > 0 {
		if crf < 18 {
			crf = 18
		}
		if crf > 40 {
			crf = 40
		}
		if vf != "" {
			args = append(args, "-vf", vf)
		}
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", fmt.Sprintf("%d", crf), "-c:a", "aac", "-b:a", "128k")
	} else if vf != "" {
		args = append(args, "-vf", vf, "-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-c:a", "aac", "-b:a", "128k")
	}
	args = append(args, "-y", outputPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ошибка ffmpeg: %v, вывод: %s", err, string(output))
	}

	return nil
}

func (c *DefaultConverter) convertVideoToAudio(ctx context.Context, inputPath, outputPath string) error {
	if !c.hasCommand("ffmpeg") {
		return fmt.Errorf("ffmpeg не установлен")
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", "-i", inputPath, "-vn", "-y", outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ошибка ffmpeg (video->audio): %v, вывод: %s", err, string(output))
	}
	return nil
}

func (c *DefaultConverter) convertVideoToGif(ctx context.Context, inputPath, outputPath string, options map[string]interface{}) error {
	if !c.hasCommand("ffmpeg") {
		return fmt.Errorf("ffmpeg не установлен")
	}

	height := 480
	if hasVideoOptions(options) {
		op, _ := optString(options, "vid_op")
		if op == "gif" {
			if h, ok := optInt(options, "vid_gif_height"); ok && h > 0 {
				height = h
			}
		}
	}
	if height < 120 {
		height = 120
	}
	if height > 1080 {
		height = 1080
	}
	filter := fmt.Sprintf("fps=12,scale=-2:%d:flags=lanczos,split[s0][s1];[s0]palettegen[p];[s1][p]paletteuse", height)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-i", inputPath, "-vf", filter, "-loop", "0", "-y", outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ошибка ffmpeg (video->gif): %v, вывод: %s", err, string(output))
	}
	return nil
}

func hasVideoOptions(options map[string]interface{}) bool {
	if options == nil {
		return false
	}
	if _, ok := options["vid_op"]; ok {
		return true
	}
	if _, ok := options["vid_height"]; ok {
		return true
	}
	if _, ok := options["vid_crf"]; ok {
		return true
	}
	if _, ok := options["vid_gif_height"]; ok {
		return true
	}
	if _, ok := options["vid_w"]; ok {
		return true
	}
	if _, ok := options["vid_h"]; ok {
		return true
	}
	return false
}

func (c *DefaultConverter) convertDocument(ctx context.Context, inputPath, outputPath string, originalExt, targetExt string) error {
	if (c.isOfficeFormat(originalExt) || originalExt == "odt" || originalExt == "rtf" || originalExt == "txt") &&
		(c.isOfficeFormat(targetExt) || targetExt == "pdf" || targetExt == "rtf" || targetExt == "txt") {
		if c.hasCommand("libreoffice") || c.hasCommand("soffice") {
			return c.convertWithLibreOffice(ctx, inputPath, outputPath, targetExt)
		}
		return fmt.Errorf("LibreOffice не установлен")
	}

	if c.isPdfFormat(originalExt) && targetExt == "txt" {
		if c.hasCommand("pdftotext") {
			return c.convertPdfToOffice(ctx, inputPath, outputPath, targetExt)
		}
		return fmt.Errorf("poppler-utils не установлен")
	}

	if c.isPdfFormat(originalExt) && c.isOfficeFormat(targetExt) {
		return fmt.Errorf("конвертация PDF в Office форматы не поддерживается напрямую")
	}

	return fmt.Errorf("конвертация документов из %s в %s не поддерживается", originalExt, targetExt)
}

func (c *DefaultConverter) libreOfficeConvertToArg(targetExt string) (convertTo string, expectedExt string, err error) {
	targetExt = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(targetExt), "."))
	if targetExt == "" {
		return "", "", fmt.Errorf("пустой целевой формат для LibreOffice")
	}

	switch targetExt {
	case "txt":
		return "txt:Text", "txt", nil
	default:
		return targetExt, targetExt, nil
	}
}

func (c *DefaultConverter) convertWithLibreOffice(ctx context.Context, inputPath, outputPath string, targetExt string) error {
	cmdName := "libreoffice"
	if !c.hasCommand("libreoffice") {
		cmdName = "soffice"
	}

	outputDir := filepath.Dir(outputPath)
	convertTo, expectedExt, err := c.libreOfficeConvertToArg(targetExt)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, cmdName, "--headless", "--convert-to", convertTo, "--outdir", outputDir, inputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ошибка LibreOffice: %v, вывод: %s", err, string(output))
	}

	baseName := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	generated := filepath.Join(outputDir, baseName+"."+expectedExt)
	if _, err := os.Stat(generated); os.IsNotExist(err) {
		generatedAlt := filepath.Join(outputDir, baseName+"."+strings.ToUpper(expectedExt))
		if _, err2 := os.Stat(generatedAlt); err2 == nil {
			generated = generatedAlt
		} else {
			return fmt.Errorf("LibreOffice не создал файл: %s\nвывод: %s", generated, string(output))
		}
	}

	if filepath.Clean(generated) == filepath.Clean(outputPath) {
		return nil
	}
	return c.copyFile(generated, outputPath)
}

func (c *DefaultConverter) convertEbook(ctx context.Context, inputPath, outputPath string, originalExt, targetExt string) error {
	_ = originalExt
	_ = targetExt
	cmd := exec.CommandContext(ctx, "ebook-convert", inputPath, outputPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ошибка Calibre: %v, вывод: %s", err, string(output))
	}
	return nil
}

func (c *DefaultConverter) convertPdfToOffice(ctx context.Context, inputPath, outputPath string, targetExt string) error {
	if targetExt == "txt" {
		cmd := exec.CommandContext(ctx, "pdftotext", inputPath, outputPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ошибка pdftotext: %v, вывод: %s", err, string(output))
		}
		return nil
	}
	return fmt.Errorf("конвертация PDF в %s не поддерживается", targetExt)
}

func (c *DefaultConverter) hasCommand(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func (c *DefaultConverter) isImageFormat(ext string) bool {
	imageFormats := []string{"png", "jpg", "jpeg", "jp2", "webp", "bmp", "tif", "tiff", "gif", "ico", "heic", "avif", "tgs", "psd", "svg", "apng", "eps"}
	return c.contains(imageFormats, ext)
}

func (c *DefaultConverter) isAudioFormat(ext string) bool {
	audioFormats := []string{"mp3", "ogg", "opus", "wav", "flac", "wma", "oga", "m4a", "aac", "aiff", "amr"}
	return c.contains(audioFormats, ext)
}

func (c *DefaultConverter) isVideoFormat(ext string) bool {
	videoFormats := []string{"mp4", "avi", "wmv", "mkv", "3gp", "3gpp", "mpg", "mpeg", "webm", "ts", "mov", "flv", "asf", "vob"}
	return c.contains(videoFormats, ext)
}

func (c *DefaultConverter) isDocumentFormat(ext string) bool {
	return c.isOfficeFormat(ext) || c.isPdfFormat(ext) || ext == "txt" || ext == "rtf" || ext == "torrent"
}

func (c *DefaultConverter) isOfficeFormat(ext string) bool {
	officeFormats := []string{"xlsx", "xls", "doc", "docx", "odt", "ods", "ppt", "pptx", "pptm", "pps", "ppsx", "ppsm", "pot", "potx", "potm", "odp"}
	return c.contains(officeFormats, ext)
}

func (c *DefaultConverter) isPdfFormat(ext string) bool {
	return ext == "pdf"
}

func (c *DefaultConverter) isEbookFormat(ext string) bool {
	ebookFormats := []string{"epub", "mobi", "azw3", "lrf", "pdb", "cbr", "fb2", "cbz", "djvu"}
	return c.contains(ebookFormats, ext)
}

func (c *DefaultConverter) contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) {
			return true
		}
	}
	return false
}

func buildResultFileName(originalName string, targetExt string) string {
	targetExt = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(targetExt), "."))
	if targetExt == "" {
		targetExt = "bin"
	}

	originalName = strings.TrimSpace(originalName)
	if originalName == "" {
		return "converted." + targetExt
	}

	base := filepath.Base(originalName)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(base)), ".")
	if ext == "" {
		return base + "." + targetExt
	}
	return strings.TrimSuffix(base, filepath.Ext(base)) + "." + targetExt
}
