package xiaohongshu

import (
	"context"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

// DraftItem 草稿项信息
type DraftItem struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	ImageURLs []string  `json:"image_urls,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DraftListResponse 草稿列表响应
type DraftListResponse struct {
	Drafts []DraftItem `json:"drafts"`
	Count  int         `json:"count"`
}

type DraftsAction struct {
	page *rod.Page
}

const urlOfDrafts = `https://creator.xiaohongshu.com/publish/publish?source=draft`

func NewDraftsAction(page *rod.Page) (*DraftsAction, error) {
	pp := page.Timeout(60 * time.Second)

	// 先访问发布页面确保登录状态有效
	logrus.Info("先访问发布页面确保登录...")
	if err := pp.Navigate(urlOfPublic); err != nil {
		return nil, errors.Wrap(err, "导航到发布页面失败")
	}
	if err := pp.WaitLoad(); err != nil {
		logrus.Warnf("等待发布页面加载出现问题: %v，继续尝试", err)
	}
	time.Sleep(2 * time.Second)

	// 检查是否在登录页
	if isOnLoginPage(pp) {
		return nil, errors.New("未登录，请先登录小红书")
	}

	// 再导航到草稿箱页面
	logrus.Info("导航到草稿箱页面...")
	if err := pp.Navigate(urlOfDrafts); err != nil {
		return nil, errors.Wrap(err, "导航到草稿箱页面失败")
	}

	// 等待页面加载
	if err := pp.WaitLoad(); err != nil {
		logrus.Warnf("等待草稿箱页面加载出现问题: %v，继续尝试", err)
	}
	time.Sleep(2 * time.Second)

	// 等待页面稳定
	if err := pp.WaitDOMStable(time.Second, 0.1); err != nil {
		logrus.Warnf("等待草稿箱 DOM 稳定出现问题: %v，继续尝试", err)
	}
	time.Sleep(1 * time.Second)

	return &DraftsAction{
		page: pp,
	}, nil
}

// isOnLoginPage 检查当前是否在登录页面
func isOnLoginPage(page *rod.Page) bool {
	url := page.MustInfo().URL
	return strings.Contains(url, "/login") || strings.Contains(url, "redirectReason=401")
}

// GetDraftsList 获取草稿列表
func (d *DraftsAction) GetDraftsList(ctx context.Context) (*DraftListResponse, error) {
	page := d.page.Context(ctx)

	logrus.Info("开始获取草稿列表...")

	// 等待草稿列表加载
	time.Sleep(2 * time.Second)

	// 尝试获取草稿数据
	drafts, err := d.extractDrafts(page)
	if err != nil {
		logrus.Warnf("提取草稿数据失败: %v", err)
		// 如果提取失败，返回空列表而不是错误
		return &DraftListResponse{
			Drafts: []DraftItem{},
			Count:  0,
		}, nil
	}

	logrus.Infof("成功获取 %d 条草稿", len(drafts))

	return &DraftListResponse{
		Drafts: drafts,
		Count:  len(drafts),
	}, nil
}

// extractDrafts 从页面提取草稿数据。用 go-rod 原生 API 探测候选选择器，
// 命中后逐个解析标题/内容/图片/时间，不做 JS 注入。
func (d *DraftsAction) extractDrafts(page *rod.Page) ([]DraftItem, error) {
	candidates := []string{
		".draft-item",
		".draft-card",
		".note-item",
		`[class*="draft"]`,
	}

	for _, sel := range candidates {
		elems, err := page.Timeout(5 * time.Second).Elements(sel)
		if err != nil || len(elems) == 0 {
			continue
		}

		drafts := make([]DraftItem, 0, len(elems))
		for _, el := range elems {
			item := DraftItem{}

			// 标题：优先明确 class，退回常见标题标签
			titleEl, err := el.Element(`.title, .note-title, h3, h4, [class*="title"]`)
			if err == nil && titleEl != nil {
				item.Title = strings.TrimSpace(titleEl.MustText())
			}

			// 内容
			contentEl, err := el.Element(`.content, .desc, .description, [class*="content"]`)
			if err == nil && contentEl != nil {
				item.Content = strings.TrimSpace(contentEl.MustText())
			}

			// 图片 URL
			imgs, err := el.Elements("img")
			if err == nil {
				for _, img := range imgs {
					if src, err := img.Attribute("src"); err == nil && src != nil && *src != "" {
						item.ImageURLs = append(item.ImageURLs, *src)
					}
				}
			}

			// ID：data-id 属性优先
			if id, err := el.Attribute("data-id"); err == nil && id != nil {
				item.ID = *id
			}

			drafts = append(drafts, item)
		}

		if len(drafts) > 0 {
			logrus.Infof("提取到 %d 条草稿（选择器: %s）", len(drafts), sel)
			return drafts, nil
		}
	}

	return nil, errors.New("未找到草稿列表元素")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
