package library

import "testing"

// TestIdentifySourceApp 穷举来源 App 识别的正例与无命中：
// 覆盖各类命名模式（截图/微信/QQ/相机等）及不应命中的普通文件名。
func TestIdentifySourceApp(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		want     string
	}{
		// 截图：各平台前缀
		{"安卓截图前缀", "Screenshot_20240101-123456.png", SourceScreenshot},
		{"安卓截图中文", "屏幕截图_20240101_123456.png", SourceScreenshot},
		{"iOS 截图中文", "IMG_截屏.png", SourceScreenshot},
		{"截图英文小写", "screenshot-2024.jpg", SourceScreenshot},

		// 微信：导出/图片/视频
		{"微信导出图片", "mmexport1700000000000.jpg", SourceWeChat},
		{"微信图片中文", "微信图片_20240101123456.jpg", SourceWeChat},
		{"微信英文前缀", "WeChat_1700000000.mp4", SourceWeChat},
		{"微信视频中文", "微信视频_20240101.mp4", SourceWeChat},

		// QQ
		{"QQ图片中文", "QQ图片20240101123456.jpg", SourceQQ},
		{"QQ导出英文", "mmexport_qq.jpg", SourceWeChat}, // mmexport 优先判微信
		{"QQ前缀英文", "QQ_image_123.jpg", SourceQQ},

		// 相机原图
		{"相机IMG", "IMG_20240101_123456.jpg", SourceCamera},
		{"相机DSC", "DSC01234.JPG", SourceCamera},
		{"相机DCIM", "DCIM0001.jpg", SourceCamera},
		{"相机VID", "VID_20240101_123456.mp4", SourceCamera},

		// 无命中：返回空，不显示
		{"普通文件名", "我的旅行照片.jpg", ""},
		{"随机命名", "a1b2c3.png", ""},
		{"空文件名", "", ""},
		{"仅扩展名", ".jpg", ""},
		{"带路径但普通名", "holiday-beach.mov", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := IdentifySourceApp(c.filename)
			if got != c.want {
				t.Errorf("IdentifySourceApp(%q) = %q, 期望 %q", c.filename, got, c.want)
			}
		})
	}
}

// TestIdentifySourceAppStripsPath 校验：传入含目录的完整路径时，仅按基名（文件名）判定。
func TestIdentifySourceAppStripsPath(t *testing.T) {
	got := IdentifySourceApp("/data/media/DCIM/Camera/IMG_20240101_120000.jpg")
	if got != SourceCamera {
		t.Errorf("含路径输入应按基名判定，得 %q 期望 %q", got, SourceCamera)
	}
}

// TestIdentifySourceAppCaseInsensitive 校验：前缀匹配大小写不敏感。
func TestIdentifySourceAppCaseInsensitive(t *testing.T) {
	if got := IdentifySourceApp("WECHAT_123.jpg"); got != SourceWeChat {
		t.Errorf("大写 WECHAT 应命中微信，得 %q", got)
	}
	if got := IdentifySourceApp("img_20240101.jpg"); got != SourceCamera {
		t.Errorf("小写 img 应命中相机，得 %q", got)
	}
}
