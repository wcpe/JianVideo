package library

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	imageMetadataReadLimit   = 32 * 1024 * 1024
	imageMetadataToolVersion = "imagemeta-v1.0.0+stdlib-v1"
)

var xmpNamespacePrefixes = map[string]string{
	"http://purl.org/dc/elements/1.1/":            "dc",
	"http://ns.adobe.com/photoshop/1.0/":          "photoshop",
	"http://ns.adobe.com/xap/1.0/":                "xmp",
	"http://ns.adobe.com/exif/1.0/":               "exif",
	"http://ns.adobe.com/tiff/1.0/":               "tiff",
	"http://www.w3.org/1999/02/22-rdf-syntax-ns#": "rdf",
}

var iptcDatasetNames = map[byte]string{
	5: "object_name", 25: "keywords", 55: "date_created", 60: "time_created",
	80: "byline", 90: "city", 95: "state", 101: "country", 105: "headline",
	110: "credit", 116: "copyright", 120: "caption",
}

func parseEmbeddedImageMetadata(path string) (ParsedEmbeddedMetadata, error) {
	exif := ExtractImageEXIF(path)
	supplemental, err := extractImageSupplementalMetadata(path)
	if err != nil {
		return ParsedEmbeddedMetadata{}, err
	}
	normalized := normalizedImageMetadata(exif, supplemental)
	rawJSON, err := marshalMetadataJSON(map[string]any{"exif": normalized.Image.EXIF, "iptc": supplemental.IPTC, "xmp": supplemental.XMP})
	if err != nil {
		return ParsedEmbeddedMetadata{}, err
	}
	normalizedJSON, err := marshalMetadataJSON(normalized)
	if err != nil {
		return ParsedEmbeddedMetadata{}, err
	}
	return ParsedEmbeddedMetadata{
		Source: MetadataSourceImage, Tool: "imagemeta+stdlib", ToolVersion: imageMetadataToolVersion,
		RawJSON: rawJSON, NormalizedJSON: normalizedJSON, Normalized: normalized,
	}, nil
}

func normalizedImageMetadata(exif *ImageEXIF, supplemental ImageSupplementalMetadata) NormalizedEmbeddedMetadata {
	image := &ImageEmbeddedMetadata{EXIF: imageEXIFMap(exif), IPTC: supplemental.IPTC, XMP: supplemental.XMP}
	return NormalizedEmbeddedMetadata{
		MediaType: MediaTypeImage,
		Image:     image,
		Tags:      imageEmbeddedTags(supplemental),
	}
}

func imageEXIFMap(exif *ImageEXIF) map[string]any {
	if exif == nil {
		return nil
	}
	result := map[string]any{
		"camera": exif.Camera, "lens": exif.Lens, "aperture": exif.Aperture,
		"shutter": exif.Shutter, "iso": exif.ISO, "gps_lat": exif.GPSLat, "gps_lon": exif.GPSLon,
	}
	if !exif.Taken.IsZero() {
		result["taken_at"] = exif.Taken
	}
	return omitEmptyImageEXIF(result)
}

func omitEmptyImageEXIF(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case string:
			if typed != "" {
				output[key] = typed
			}
		case int:
			if typed != 0 {
				output[key] = typed
			}
		case float64:
			if typed != 0 {
				output[key] = typed
			}
		default:
			output[key] = value
		}
	}
	return output
}

func imageEmbeddedTags(metadata ImageSupplementalMetadata) map[string]string {
	tags := map[string]string{}
	copyFirstTag(tags, "title", metadata.XMP["dc:title"], metadata.IPTC["object_name"], metadata.IPTC["headline"])
	copyFirstTag(tags, "author", metadata.XMP["dc:creator"], metadata.IPTC["byline"])
	copyFirstTag(tags, "description", metadata.XMP["dc:description"], metadata.IPTC["caption"])
	copyFirstTag(tags, "copyright", metadata.XMP["dc:rights"], metadata.IPTC["copyright"])
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func copyFirstTag(tags map[string]string, key string, values ...string) {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			tags[key] = value
			return
		}
	}
}

func extractImageSupplementalMetadata(path string) (ImageSupplementalMetadata, error) {
	data, err := readImageMetadataPrefix(path)
	if err != nil {
		return ImageSupplementalMetadata{}, err
	}
	segments := jpegMetadataSegments(data)
	xmp := parseXMPPackets(append(segments.app1, data...))
	iptc := parseIPTCDatasets(segments.app13)
	return ImageSupplementalMetadata{IPTC: iptc, XMP: xmp}, nil
}

func readImageMetadataPrefix(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开图片元数据失败: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, imageMetadataReadLimit))
	if err != nil {
		return nil, fmt.Errorf("读取图片元数据失败: %w", err)
	}
	return data, nil
}

type jpegSegments struct {
	app1  []byte
	app13 []byte
}

func jpegMetadataSegments(data []byte) jpegSegments {
	var result jpegSegments
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return result
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			offset++
			continue
		}
		marker := data[offset+1]
		if marker == 0xda || marker == 0xd9 {
			break
		}
		length := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		end := offset + 2 + length
		if length < 2 || end > len(data) {
			break
		}
		payload := data[offset+4 : end]
		if marker == 0xe1 {
			result.app1 = append(result.app1, payload...)
		}
		if marker == 0xed {
			result.app13 = append(result.app13, payload...)
		}
		offset = end
	}
	return result
}

func parseXMPPackets(data []byte) map[string]string {
	packets := xmpPackets(data)
	result := map[string]string{}
	for _, packet := range packets {
		mergeStringMap(result, decodeXMP(packet))
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func xmpPackets(data []byte) [][]byte {
	packets := make([][]byte, 0, 1)
	startToken, endToken := []byte("<x:xmpmeta"), []byte("</x:xmpmeta>")
	for offset := 0; offset < len(data); {
		start := bytes.Index(data[offset:], startToken)
		if start < 0 {
			break
		}
		start += offset
		end := bytes.Index(data[start:], endToken)
		if end < 0 {
			break
		}
		end += start + len(endToken)
		packets = append(packets, data[start:end])
		offset = end
	}
	return packets
}

func decodeXMP(packet []byte) map[string]string {
	decoder := xml.NewDecoder(bytes.NewReader(packet))
	result := map[string]string{}
	stack := make([]xml.Name, 0, 8)
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch typed := token.(type) {
		case xml.StartElement:
			stack = append(stack, typed.Name)
			collectXMPAttributes(result, typed)
		case xml.CharData:
			collectXMPText(result, stack, string(typed))
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return result
}

func collectXMPAttributes(result map[string]string, element xml.StartElement) {
	for _, attr := range element.Attr {
		key := xmpName(attr.Name)
		if key == "" || strings.HasPrefix(key, "rdf:") {
			continue
		}
		if value := strings.TrimSpace(attr.Value); value != "" {
			result[key] = value
		}
	}
}

func collectXMPText(result map[string]string, stack []xml.Name, raw string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return
	}
	for i := len(stack) - 1; i >= 0; i-- {
		key := xmpName(stack[i])
		if key == "" || strings.HasPrefix(key, "rdf:") || key == "x:xmpmeta" {
			continue
		}
		if previous := result[key]; previous != "" && previous != value {
			result[key] = previous + ", " + value
		} else {
			result[key] = value
		}
		return
	}
}

func xmpName(name xml.Name) string {
	prefix := xmpNamespacePrefixes[name.Space]
	if prefix == "" {
		if name.Space == "adobe:ns:meta/" {
			prefix = "x"
		} else if name.Space != "" {
			return ""
		}
	}
	if prefix == "" {
		return name.Local
	}
	return prefix + ":" + name.Local
}

func parseIPTCDatasets(data []byte) map[string]string {
	result := map[string]string{}
	for offset := 0; offset+5 <= len(data); {
		if data[offset] != 0x1c || data[offset+1] != 0x02 {
			offset++
			continue
		}
		dataset, length := data[offset+2], int(binary.BigEndian.Uint16(data[offset+3:offset+5]))
		start, end := offset+5, offset+5+length
		if end > len(data) {
			break
		}
		appendIPTCValue(result, iptcDatasetNames[dataset], string(data[start:end]))
		offset = end
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func appendIPTCValue(result map[string]string, key, raw string) {
	value := strings.TrimSpace(raw)
	if key == "" || value == "" {
		return
	}
	if previous := result[key]; previous != "" && previous != value {
		result[key] = previous + ", " + value
		return
	}
	result[key] = value
}

func mergeStringMap(target, source map[string]string) {
	for key, value := range source {
		if strings.TrimSpace(value) != "" {
			target[key] = value
		}
	}
}

func validMetadataJSON(raw string) bool {
	return json.Valid([]byte(raw))
}
