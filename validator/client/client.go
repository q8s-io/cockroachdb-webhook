package client

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

const (
	defaultUseServiceAccount = true
	defaultInClusterConfig   = true
)

type UrlInfo struct {
	InClusterConfig   bool
	Insecure          bool
	UseServiceAccount bool
	Server            string
	AuthFile          string
}

func GetInfoFromUrl(uri string) (*UrlInfo, error) {
	// init default value
	var urlInfo = UrlInfo{
		InClusterConfig:   defaultInClusterConfig,
		Insecure:          false,
		UseServiceAccount: defaultUseServiceAccount,
		Server:            "",
	}
	requestURI, parseErr := url.ParseRequestURI(uri)
	if parseErr != nil {
		klog.Error(parseErr)
		return nil, parseErr
	}

	if len(requestURI.Scheme) != 0 && len(requestURI.Host) != 0 {
		urlInfo.Server = fmt.Sprintf("%s://%s", requestURI.Scheme, requestURI.Host)
	}
	opts := requestURI.Query()
	fmt.Println("opts--------------", opts)
	if len(opts["inClusterConfig"]) > 0 {
		inClusterConfig, err := strconv.ParseBool(opts["inClusterConfig"][0])
		if err != nil {
			klog.Error(err)
			return nil, err
		}
		urlInfo.InClusterConfig = inClusterConfig
	}

	if len(opts["insecure"]) > 0 {
		insecure, err := strconv.ParseBool(opts["insecure"][0])
		if err != nil {
			klog.Error(err)
			return nil, err
		}
		// kubeConfigOverride.ClusterInfo.InsecureSkipTLSVerify = insecure
		urlInfo.Insecure = insecure
	}

	if len(opts["useServiceAccount"]) >= 1 {
		useServiceAccount, err := strconv.ParseBool(opts["useServiceAccount"][0])
		if err != nil {
			klog.Error(err)
			return nil, err
		}
		urlInfo.UseServiceAccount = useServiceAccount
	}

	if !urlInfo.InClusterConfig {
		authFile := ""
		if len(opts["auth"]) > 0 {
			authFile = opts["auth"][0]
		}
		urlInfo.AuthFile = authFile
	}

	return &urlInfo, nil

}

func ConvertKubeCfg(urlInfo *UrlInfo) (*rest.Config, error) {
	var kubeConfig *rest.Config
	var err error

	if urlInfo.InClusterConfig {
		kubeConfig, err = rest.InClusterConfig()
		if err != nil {
			klog.Error(err)
			return nil, err
		}
		if urlInfo.Server != "" {
			kubeConfig.Host = urlInfo.Server
		}
		kubeConfig.GroupVersion = &schema.GroupVersion{Version: "v1"}
		kubeConfig.Insecure = urlInfo.Insecure
		if urlInfo.Insecure {
			kubeConfig.TLSClientConfig.CAFile = ""
		}
	} else {
		if urlInfo.AuthFile != "" {
			// Load structured kubeconfig data from the given path.
			loader := &clientcmd.ClientConfigLoadingRules{ExplicitPath: urlInfo.AuthFile}
			loadedConfig, err := loader.Load()
			if err != nil {
				klog.Error(err)
				return nil, err
			}
			// Flatten the loaded data to a particular restclient.Config based on the current context.
			if kubeConfig, err = clientcmd.NewNonInteractiveClientConfig(
				*loadedConfig,
				loadedConfig.CurrentContext,
				&clientcmd.ConfigOverrides{},
				loader).ClientConfig(); err != nil {
				klog.Error(err)
				return nil, err
			}
		} else {
			kubeConfig = &rest.Config{
				Host: urlInfo.Server,
				TLSClientConfig: rest.TLSClientConfig{
					Insecure: urlInfo.Insecure,
				},
			}
			kubeConfig.GroupVersion = &schema.GroupVersion{Version: "v1"}
		}
	}
	if len(kubeConfig.Host) == 0 {
		return nil, fmt.Errorf("invalid kubernetes master url specified")
	}
	if urlInfo.UseServiceAccount {
		// If a readable service account token exists, then use it
		fmt.Println("--------------get token")
		if contents, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
			kubeConfig.BearerToken = string(contents)
			fmt.Println(string(contents))
		} else {
			fmt.Println("read file token error: ", err.Error())
		}
	}
	fmt.Println("------------------", kubeConfig.BearerToken)
	kubeConfig.ContentType = "application/vnd.kubernetes.protobuf"
	return kubeConfig, nil
}
