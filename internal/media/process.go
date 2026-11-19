package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	"github.com/rwcarlsen/goexif/exif"
	"golang.org/x/image/webp"
)

var (
	ErrUnsupported = errors.New("不支持的图片格式")
	ErrTooLarge    = errors.New("图片尺寸过大")
)

const maxPixels = 120 * 1000 * 1000

// Result 是一张上传图片的派生结果。
type Result struct {
	Width      int
	Height     int
	Mime       string
	Ext        string
	TakenAt    int64
	HasTakenAt bool
	File       []byte // 实际保存的可下载文件；HEIC 为转换后的 JPEG
	Thumb      []byte // 列表缩略图，长边 480
	Preview    []byte // 全屏预览图，长边 1600
}

// Process 解码图片、读取拍摄时间并生成两档缩略图。
func Process(data []byte, filename string) (*Result, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	var (
		img         image.Image
		err         error
		exifSrc     = data
		deliverable = data
	)
	switch ext {
	case ".jpg", ".jpeg":
		img, err = imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	case ".png":
		img, err = imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	case ".webp":
		img, err = webp.Decode(bytes.NewReader(data))
	case ".heic", ".heif":
		jpg, convErr := convertHEIC(data)
		if convErr != nil {
			return nil, convErr
		}
		exifSrc = jpg
		deliverable = jpg
		ext = ".jpg"
		img, err = imaging.Decode(bytes.NewReader(jpg), imaging.AutoOrientation(true))
	default:
		return nil, ErrUnsupported
	}
	if err != nil {
		return nil, fmt.Errorf("图片解码失败: %w", err)
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, ErrUnsupported
	}
	if w*h > maxPixels {
		return nil, ErrTooLarge
	}

	mime := "image/jpeg"
	switch ext {
	case ".png":
		mime = "image/png"
	case ".webp":
		mime = "image/webp"
	}
	if ext == ".jpeg" {
		ext = ".jpg"
	}

	r := &Result{Width: w, Height: h, Mime: mime, Ext: ext, File: deliverable}
	if t, ok := readTakenAt(exifSrc); ok {
		r.TakenAt = t.Unix()
		r.HasTakenAt = true
	}

	flat := flatten(img)
	if r.Thumb, err = encodeJPEG(imaging.Fit(flat, 480, 480, imaging.Lanczos)); err != nil {
		return nil, err
	}
	if r.Preview, err = encodeJPEG(imaging.Fit(flat, 1600, 1600, imaging.Lanczos)); err != nil {
		return nil, err
	}
	return r, nil
}

func readTakenAt(data []byte) (time.Time, bool) {
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		return time.Time{}, false
	}
	dt, err := x.DateTime()
	if err != nil || dt.IsZero() {
		return time.Time{}, false
	}
	return dt, true
}

// flatten 把带透明通道的图片铺到白底上，避免转成 JPEG 后出现黑块。
func flatten(img image.Image) image.Image {
	if op, ok := img.(interface{ Opaque() bool }); ok && op.Opaque() {
		return img
	}
	b := img.Bounds()
	canvas := image.NewNRGBA(b)
	draw.Draw(canvas, b, image.White, image.Point{}, draw.Src)
	draw.Draw(canvas, b, img, b.Min, draw.Over)
	return canvas
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(82)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
