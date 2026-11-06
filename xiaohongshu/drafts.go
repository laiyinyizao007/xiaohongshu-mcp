package xiaohongshu

import (
	"context"
	"encoding/json"
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

// extractDrafts 从页面提取草稿数据
func (d *DraftsAction) extractDrafts(page *rod.Page) ([]DraftItem, error) {
	// 通过 JS 获取草稿数据，返回 JSON 字符串
	script := `() => {
		const drafts = [];
		const selectors = [
			'.draft-item',
			'.draft-card',
			'.note-item',
			'[class*="draft"]',
			'[class*="Draft"]',
		];

		for (const selector of selectors) {
			const elements = document.querySelectorAll(selector);
			if (elements.length > 0) {
				console.log('Found ' + elements.length + ' elements with selector: ' + selector);
				for (const el of elements) {
					try {
						const titleEl = el.querySelector('.title, .note-title, h3, h4, [class*="title"]');
						const title = titleEl ? titleEl.innerText.trim() : '';

						const contentEl = el.querySelector('.content, .desc, .description, [class*="content"]');
						const content = contentEl ? contentEl.innerText.trim() : '';

						const imgElements = el.querySelectorAll('img');
						const imageUrls = [];
						for (const img of imgElements) {
							if (img.src) {
								imageUrls.push(img.src);
							}
						}

						const timeEl = el.querySelector('.time, .date, [class*="time"]');
						const timeStr = timeEl ? timeEl.innerText.trim() : '';

						drafts.push({
							id: el.dataset.id || el.id || '',
							title: title,
							content: content,
							imageUrls: imageUrls,
							time: timeStr,
							selector: selector
						});
					} catch (e) {
						console.error('Error extracting draft:', e);
					}
				}
				if (drafts.length > 0) break;
			}
		}

		// 返回 JSON 字符串
		return JSON.stringify({
			drafts: drafts,
			url: window.location.href,
			title: document.title,
			bodyText: document.body ? document.body.innerText.substring(0, 300) : ''
		});
	}`

	result, err := page.Eval(script)

	if err != nil {
		return nil, errors.Wrap(err, "执行 JS 提取草稿数据失败")
	}

	// 解析 JSON 结果
	var jsResult struct {
		Drafts   []DraftItem `json:"drafts"`
		URL      string      `json:"url"`
		Title    string      `json:"title"`
		BodyText string      `json:"bodyText"`
	}

	jsonStr := result.Value.String()
	if err := json.Unmarshal([]byte(jsonStr), &jsResult); err != nil {
		logrus.Warnf("解析草稿数据失败: %v, raw: %s", err, jsonStr[:min(len(jsonStr), 200)])
		return nil, errors.Wrap(err, "解析草稿数据失败")
	}

	// 记录页面信息用于调试
	logrus.Infof("草稿箱页面: %s, 标题: %s, 草稿数: %d", jsResult.URL, jsResult.Title, len(jsResult.Drafts))
	if len(jsResult.Drafts) == 0 {
		logrus.Warnf("未找到草稿，页面内容预览: %s", jsResult.BodyText)
	}

	return jsResult.Drafts, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
