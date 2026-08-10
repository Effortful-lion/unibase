package provider

// ProviderOption 配置 Provider 创建参数。
type ProviderOption func(*ProviderOptions)

// ProviderOptions 是创建 Provider 时的配置。
type ProviderOptions struct {
	ProviderName string
	ModelName    string
	APIKey       string
	BaseURL      string
	Headers      map[string]string
	Extra        map[string]any
}

func WithProviderName(name string) ProviderOption {
	return func(o *ProviderOptions) { o.ProviderName = name }
}

func WithModelName(name string) ProviderOption {
	return func(o *ProviderOptions) { o.ModelName = name }
}

func WithAPIKey(key string) ProviderOption {
	return func(o *ProviderOptions) { o.APIKey = key }
}

func WithBaseURL(url string) ProviderOption {
	return func(o *ProviderOptions) { o.BaseURL = url }
}

func WithHeaders(headers map[string]string) ProviderOption {
	return func(o *ProviderOptions) {
		if o.Headers == nil {
			o.Headers = make(map[string]string)
		}
		for k, v := range headers {
			o.Headers[k] = v
		}
	}
}

func WithExtra(extra map[string]any) ProviderOption {
	return func(o *ProviderOptions) {
		if o.Extra == nil {
			o.Extra = make(map[string]any)
		}
		for k, v := range extra {
			o.Extra[k] = v
		}
	}
}
