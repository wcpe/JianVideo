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
}

// BrowseResponse 目录浏览响应。
type BrowseResponse struct {
	Breadcrumbs []BreadcrumbItem `json:"breadcrumbs"`
	Directories []DirInfo         `json:"directories"`
	Files       []MediaFile      `json:"files"`
}
