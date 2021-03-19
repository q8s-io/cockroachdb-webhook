package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	whhttp "github.com/slok/kubewebhook/pkg/http"
	"github.com/slok/kubewebhook/pkg/log"
	validatingwh "github.com/slok/kubewebhook/pkg/webhook/validating"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"

	client "github.com/q8s.io/cockroadchdb-webhook/validator/client"

	// extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const url = "https://kubernetes.default"

type podValidator struct {
	logger log.Logger
	client kubernetes.Clientset
	config rest.Config
}

func (v *podValidator) Validate(_ context.Context, obj metav1.Object) (bool, validatingwh.ValidatorResult, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return false, validatingwh.ValidatorResult{}, fmt.Errorf("not an pod")
	}
	if _, ok := pod.Annotations["cockrochDB"]; !ok {
		return false, validatingwh.ValidatorResult{Valid: true, Message: "does not match cockrochDB"}, nil
	}
	v.logger.Infof("find pod match cockrochDB")
	if _, ok := pod.Annotations["nodeUnhealthy"]; !ok {
		return false, validatingwh.ValidatorResult{Valid: true, Message: "node is healthy"}, nil
	}
	v.logger.Infof("cockrochDB node unhealthy")
	normalPod, err := v.getPod(pod.Namespace)
	if err != nil {
		v.logger.Errorf("get normalPod error: %v", err.Error())
		return false, validatingwh.ValidatorResult{}, err
	}
	if normalPod == nil {
		v.logger.Infof("no pod is ready")
	}
	if err := v.deleteCoDBID(pod, normalPod); err != nil {
		v.logger.Errorf("delete coDBID error: %v", err.Error())
		return false, validatingwh.ValidatorResult{}, err
	}
	res := validatingwh.ValidatorResult{
		Valid:   true,
		Message: "delete codbId successful",
	}
	return false, res, nil
}

func (v *podValidator) deleteCoDBID(errorPod *corev1.Pod, normalPod *corev1.Pod) error {
	ctx, cancle := context.WithTimeout(context.Background(), time.Duration(time.Minute*5))
	deleteErr := make(chan struct{})
	defer cancle()
	go func(ctx context.Context) {
		arg := fmt.Sprintf("cockroach node status --certs-dir=cockroach-certs/ | grep %v |awk '{print $1}' |tail -1", errorPod.Name)
		req := v.client.CoreV1().RESTClient().Post().Resource("pods").Name(normalPod.Name).Namespace(normalPod.Namespace).SubResource("exec")
		options := &corev1.PodExecOptions{
			Command: []string{"/bin/bash", "-c", "id=" + "$(" + arg + ");cockroach --certs-dir=cockroach-certs/ node decommission $id"},
			Stdin:   true,
			Stdout:  true,
			Stderr:  true,
			TTY:     true,
		}
		req.VersionedParams(options, scheme.ParameterCodec)
		exec, err := remotecommand.NewSPDYExecutor(&v.config, "POST", req.URL())
		if err != nil {
			v.logger.Errorf("exec error: %v", err.Error())
			deleteErr <- struct{}{}
			return
		}
		err = exec.Stream(remotecommand.StreamOptions{
			Stdin:  os.Stdin,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		})
		if err != nil {
			v.logger.Errorf("exec stream error: %v", err.Error())
			deleteErr <- struct{}{}
			return
		}
		cancle()
	}(ctx)
	select {
	case <-ctx.Done():
		v.logger.Infof("delete coDB id successfully")
		return nil
	case <-time.After(time.Minute*5 + time.Second*10):
		//	v.logger.Errorf("time out!")
		return errors.New("time out")
	case <-deleteErr:
		return errors.New("delete error")
	}
}

func (v *podValidator) getPod(namespace string) (*corev1.Pod, error) {
	pods, err := v.client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		v.logger.Errorf("list pod error: %v", err.Error())
		return nil, err
	}
	for _, v := range pods.Items {
		if _, ok := v.Annotations["cockrochDB"]; !ok {
			continue
		}
		if v.Status.Phase == corev1.PodRunning && v.Status.Conditions[1].Status == corev1.ConditionTrue {
			return &v, nil
		}
	}
	return nil, nil
}

type config struct {
	certFile string
	keyFile  string
	addr     string
}

func initFlags() *config {
	cfg := &config{}

	fl := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fl.StringVar(&cfg.certFile, "tls-cert-file", "", "TLS certificate file")
	fl.StringVar(&cfg.keyFile, "tls-key-file", "", "TLS key file")
	fl.StringVar(&cfg.addr, "listen-addr", ":8080", "The address to start the server")
	//fl.StringVar()
	fl.Parse(os.Args[1:])
	return cfg
}

func main() {
	logger := &log.Std{Debug: true}

	cfg := initFlags()

	urlInfo, err := client.GetInfoFromUrl(url)
	if err != nil {
		return
	}
	clientCfg, err := client.ConvertKubeCfg(urlInfo)
	if err != nil {
		return
	}
	clientset := kubernetes.NewForConfigOrDie(clientCfg)
	vl := &podValidator{
		logger: logger,
		client: *clientset,
		config: *clientCfg,
	}
	vcfg := validatingwh.WebhookConfig{
		Name: "coDBValidator",
		Obj:  &corev1.Pod{},
	}
	wh, err := validatingwh.NewWebhook(vcfg, vl, nil, nil, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating webhook: %s", err)
		os.Exit(1)
	}

	// Serve the webhook.
	logger.Infof("Listening on %s", cfg.addr)
	err = http.ListenAndServeTLS(cfg.addr, cfg.certFile, cfg.keyFile, whhttp.MustHandlerFor(wh))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error serving webhook: %s", err)
		os.Exit(1)
	}
}
