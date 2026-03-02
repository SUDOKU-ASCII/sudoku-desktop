package core

// directDNSBootstrapIPv4 contains stable resolver IPs used by the local DNS proxy
// to perform DNS-over-HTTPS without relying on the system resolver.
//
// These are intentionally IPv4-only to keep the bootstrap path simple and avoid
// issues with IPv6 routing on some networks.
var directDNSBootstrapIPv4 = []string{
	"223.5.5.5",    // AliDNS
	"223.6.6.6",    // AliDNS
	"119.29.29.29", // DNSPod/Tencent
	"119.28.28.28", // DNSPod/Tencent
}

var (
	aliDNSBootstrapIPv4   = []string{"223.5.5.5", "223.6.6.6"}
	dnsPodBootstrapIPv4   = []string{"119.29.29.29", "119.28.28.28"}
	fallbackPlainDNSIPv4  = []string{"223.5.5.5:53", "119.29.29.29:53"}
	fallbackPlainDoHIPv4  = []string{"223.5.5.5", "119.29.29.29"}
)
