//go:build cgo && ffmpeg
// +build cgo,ffmpeg

package transcoder

/*
#include <stdlib.h>
#include <libavcodec/avcodec.h>
*/
import "C"

import "unsafe"

// findEncoderByName 通过 FFmpeg C API 查找指定名称的编码器。
// 未找到时返回 (nil, nil)。
func findEncoderByName(name string) (interface{}, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	enc := C.avcodec_find_encoder_by_name(cName)
	if enc == nil {
		return nil, nil
	}
	return enc, nil
}
