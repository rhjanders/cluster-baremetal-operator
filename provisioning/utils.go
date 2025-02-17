package provisioning

import (
	"context"
	"fmt"
	"net"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"

	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
)

func getPodHostIP(podClient coreclientv1.PodsGetter, targetNamespace string) (string, error) {
	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"k8s-app":    metal3AppName,
			cboLabelName: stateService,
		}}

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return "", err
	}

	listOptions := metav1.ListOptions{
		LabelSelector: selector.String(),
	}

	podList, err := podClient.Pods(targetNamespace).List(context.Background(), listOptions)
	if err != nil {
		return "", err
	}

	var hostIP string
	switch len(podList.Items) {
	case 0:
		// Ironic IP not available yet, just return an empty string
	case 1:
		hostIP = podList.Items[0].Status.HostIP
	default:
		// We expect only one pod with the above LabelSelector
		err = fmt.Errorf("there should be only one pod listed for the given label")
	}

	return hostIP, err
}

func GetServerInternalIP(osclient osclientset.Interface) (string, error) {
	infra, err := osclient.ConfigV1().Infrastructures().Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("Cannot get the 'cluster' object from infrastructure API: %w", err)
		return "", err
	}

	// NOTE(dtantsur): do we need to handle non-BareMetal platforms here?
	if infra.Status.PlatformStatus.BareMetal == nil {
		err = fmt.Errorf("Cannot detect server API VIP: not a baremetal platform")
		return "", err
	}

	// FIXME(dtantsur): handle the new APIServerInternalIPs field and the dualstack case.
	return infra.Status.PlatformStatus.BareMetal.APIServerInternalIP, nil
}

func GetIronicIP(client kubernetes.Interface, targetNamespace string, config *metal3iov1alpha1.ProvisioningSpec, osclient osclientset.Interface) (ironicIP string, inspectorIP string, err error) {
	// Inspector does not support proxy
	if config.ProvisioningNetwork != metal3iov1alpha1.ProvisioningNetworkDisabled && !config.VirtualMediaViaExternalNetwork {
		inspectorIP = config.ProvisioningIP
	} else {
		inspectorIP, err = getPodHostIP(client.CoreV1(), targetNamespace)
		if err != nil {
			return
		}
	}

	if UseIronicProxy(config) {
		ironicIP, err = GetServerInternalIP(osclient)
	} else {
		ironicIP = inspectorIP
	}

	return
}

func IpOptionForProvisioning(config *metal3iov1alpha1.ProvisioningSpec, networkStack NetworkStackType) string {
	var optionValue string
	ip := net.ParseIP(config.ProvisioningIP)
	if config.ProvisioningNetwork == metal3iov1alpha1.ProvisioningNetworkDisabled || ip == nil {
		// It ProvisioningNetworkDisabled or no valid IP to check, fallback to the external network
		return networkStack.IpOption()
	}
	if ip.To4() != nil {
		optionValue = "ip=dhcp"
	} else {
		optionValue = "ip=dhcp6"
	}
	return optionValue
}
