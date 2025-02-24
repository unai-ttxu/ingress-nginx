/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package proxyssl

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	networking "k8s.io/api/networking/v1"
	"k8s.io/ingress-nginx/internal/ingress/annotations/parser"
	ing_errors "k8s.io/ingress-nginx/internal/ingress/errors"
	"k8s.io/ingress-nginx/internal/ingress/resolver"
	"k8s.io/ingress-nginx/internal/k8s"
	"k8s.io/klog/v2"
)

const (
	defaultProxySSLCiphers     = "DEFAULT"
	defaultProxySSLProtocols   = "TLSv1 TLSv1.1 TLSv1.2"
	defaultProxySSLVerify      = "off"
	defaultProxySSLVerifyDepth = 1
	defaultProxySSLServerName  = "off"
)

var (
	proxySSLOnOffRegex    = regexp.MustCompile(`^(on|off)$`)
	proxySSLProtocolRegex = regexp.MustCompile(`^(SSLv2|SSLv3|TLSv1|TLSv1\.1|TLSv1\.2|TLSv1\.3)$`)
)

// Config contains the AuthSSLCert used for mutual authentication
// and the configured VerifyDepth
type Config struct {
	resolver.AuthSSLCert
	Ciphers            string `json:"ciphers"`
	Protocols          string `json:"protocols"`
	ProxySSLName       string `json:"proxySSLName"`
	Verify             string `json:"verify"`
	VerifyDepth        int    `json:"verifyDepth"`
	ProxySSLServerName string `json:"proxySSLServerName"`
}

// Equal tests for equality between two Config types
func (pssl1 *Config) Equal(pssl2 *Config) bool {
	if pssl1 == pssl2 {
		return true
	}
	if pssl1 == nil || pssl2 == nil {
		return false
	}
	if !(&pssl1.AuthSSLCert).Equal(&pssl2.AuthSSLCert) {
		return false
	}
	if pssl1.Ciphers != pssl2.Ciphers {
		return false
	}
	if pssl1.Protocols != pssl2.Protocols {
		return false
	}
	if pssl1.Verify != pssl2.Verify {
		return false
	}
	if pssl1.VerifyDepth != pssl2.VerifyDepth {
		return false
	}
	if pssl1.ProxySSLServerName != pssl2.ProxySSLServerName {
		return false
	}
	return true
}

// NewParser creates a new TLS authentication annotation parser
func NewParser(resolver resolver.Resolver) parser.IngressAnnotation {
	return proxySSL{resolver}
}

type proxySSL struct {
	r resolver.Resolver
}

func sortProtocols(protocols string) string {
	protolist := strings.Split(protocols, " ")

	n := 0
	for _, proto := range protolist {
		proto = strings.TrimSpace(proto)
		if proto == "" || !proxySSLProtocolRegex.MatchString(proto) {
			continue
		}
		protolist[n] = proto
		n++
	}

	if n == 0 {
		return defaultProxySSLProtocols
	}

	protolist = protolist[:n]
	sort.Strings(protolist)
	return strings.Join(protolist, " ")
}

// Parse parses the annotations contained in the ingress
// rule used to use a Certificate as authentication method
func (p proxySSL) Parse(ing *networking.Ingress) (interface{}, error) {
	var err error
	var secretInVault bool = true
	var proxysslsecret string
	config := &Config{}

	proxysslsecret, err = parser.GetStringAnnotation("proxy-ssl-vault", ing)
	if err != nil && err.Error() != "ingress rule without annotations" {
		klog.V(3).InfoS("There was an error in retrieving annotation proxy-ssl-vault", "error", err)
		return &Config{}, err
	}

	// If there is no vault secret use standard k8s secret
	if proxysslsecret == "" {
		klog.V(3).Infof("There was no proxy-ssl-vault secret when retrieving")
		proxysslsecret, err = parser.GetStringAnnotation("proxy-ssl-secret", ing)
		klog.V(3).Infof("Secret is parsed from k8s", "proxysslsecret", proxysslsecret)
		if err != nil {
			klog.V(3).InfoS("There was an error in retrieving annotation proxy-ssl-secret", "error", err)
			return &Config{}, err
		}
		secretInVault = false
		_, _, err = k8s.ParseNameNS(proxysslsecret)
		if err != nil {
			return &Config{}, ing_errors.NewLocationDenied(err.Error())

		}
	}
	proxyCert, err := p.r.GetAuthCertificate(proxysslsecret, secretInVault)
	if err != nil {
		e := fmt.Errorf("error obtaining certificate: %w", err)
		return &Config{}, ing_errors.LocationDenied{Reason: e}
	}
	config.AuthSSLCert = *proxyCert

	config.Ciphers, err = parser.GetStringAnnotation("proxy-ssl-ciphers", ing)
	if err != nil {
		config.Ciphers = defaultProxySSLCiphers
	}

	config.Protocols, err = parser.GetStringAnnotation("proxy-ssl-protocols", ing)
	if err != nil {
		config.Protocols = defaultProxySSLProtocols
	} else {
		config.Protocols = sortProtocols(config.Protocols)
	}

	config.ProxySSLName, err = parser.GetStringAnnotation("proxy-ssl-name", ing)
	if err != nil {
		config.ProxySSLName = ""
	}

	config.Verify, err = parser.GetStringAnnotation("proxy-ssl-verify", ing)
	if err != nil || !proxySSLOnOffRegex.MatchString(config.Verify) {
		config.Verify = defaultProxySSLVerify
	}

	config.VerifyDepth, err = parser.GetIntAnnotation("proxy-ssl-verify-depth", ing)
	if err != nil || config.VerifyDepth == 0 {
		config.VerifyDepth = defaultProxySSLVerifyDepth
	}

	config.ProxySSLServerName, err = parser.GetStringAnnotation("proxy-ssl-server-name", ing)
	if err != nil || !proxySSLOnOffRegex.MatchString(config.ProxySSLServerName) {
		config.ProxySSLServerName = defaultProxySSLServerName
	}

	return config, nil
}
