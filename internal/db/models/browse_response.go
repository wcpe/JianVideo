package models

// BreadcrumbItem 面包屑路径段。
type BreadcrumbItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// DirInfo 子目录信息。
type DirInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// LibraryID 仅聚合虚拟根（FR-66）的库目录项填充，标识点进去用哪个媒体库；
	// 普通子目录项不填（omitempty 不出现在响应中）。
	LibraryID int64 `json:"library_id,omitempty"`
}

// BrowseResponse 目录浏览响应。
type BrowseResponse struct {
	Breadcrumbs []BreadcrumbItem `json:"breadcrumbs"`
	Directories []DirInfo         `json:"directories"`
	Files       []MediaFile      `json:"files"`
}
