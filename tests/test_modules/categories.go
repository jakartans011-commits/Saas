// tests/test_modules/categories.go
package test_modules

import "fmt"

// ModuleCategory mendefinisikan satu kategori modul test.
type ModuleCategory struct {
	Name         string
	Capabilities []string
	Permissions  []string
	Count        int
}

// AllCategories adalah seluruh domain test module.
var AllCategories = []ModuleCategory{
	{
		Name: "reconnaissance",
		Capabilities: []string{
			"recon.collect.system",
			"recon.collect.network",
			"recon.collect.process",
			"recon.scan.port",
			"recon.scan.vulnerability",
		},
		Permissions: []string{"system", "network", "process"},
		Count:       10,
	},
	{
		Name: "credential",
		Capabilities: []string{
			"credential.browser.chrome",
			"credential.browser.firefox",
			"credential.dpapi.decrypt",
			"credential.lsass.dump",
			"credential.registry.read",
		},
		Permissions: []string{"storage", "crypto", "registry"},
		Count:       8,
	},
	{
		Name: "collection",
		Capabilities: []string{
			"collection.file.read",
			"collection.file.write",
			"collection.screenshot.capture",
			"collection.clipboard.read",
			"collection.keyboard.log",
		},
		Permissions: []string{"filesystem", "system"},
		Count:       10,
	},
	{
		Name: "exfiltration",
		Capabilities: []string{
			"exfil.network.http",
			"exfil.network.dns",
			"exfil.cloud.s3",
			"exfil.cloud.azure",
			"exfil.email.smtp",
		},
		Permissions: []string{"network", "cloud"},
		Count:       6,
	},
	{
		Name: "persistence",
		Capabilities: []string{
			"persistence.registry.run",
			"persistence.schtask.create",
			"persistence.service.install",
			"persistence.wmi.subscribe",
			"persistence.startup.folder",
		},
		Permissions: []string{"registry", "system"},
		Count:       8,
	},
	{
		Name: "lateral",
		Capabilities: []string{
			"lateral.wmi.exec",
			"lateral.rdp.connect",
			"lateral.smb.exec",
			"lateral.ssh.connect",
			"lateral.psremoting.exec",
		},
		Permissions: []string{"network", "process"},
		Count:       6,
	},
	{
		Name: "impact",
		Capabilities: []string{
			"impact.encryption.file",
			"impact.encryption.disk",
			"impact.ransomware.exec",
			"impact.defender.disable",
			"impact.firewall.disable",
		},
		Permissions: []string{"filesystem", "crypto", "system"},
		Count:       5,
	},
	{
		Name: "hardware",
		Capabilities: []string{
			"hardware.camera.read",
			"hardware.camera.stream",
			"hardware.usb.read",
			"hardware.usb.write",
			"hardware.gps.read",
		},
		Permissions: []string{"hardware"},
		Count:       10,
	},
	{
		Name: "cloud",
		Capabilities: []string{
			"cloud.aws.s3.read",
			"cloud.aws.s3.write",
			"cloud.azure.blob.read",
			"cloud.gcp.storage.read",
			"cloud.aws.ec2.describe",
		},
		Permissions: []string{"cloud"},
		Count:       10,
	},
	{
		Name: "ai",
		Capabilities: []string{
			"ai.inference.run",
			"ai.model.load",
			"ai.model.train",
			"ai.embedding.generate",
			"ai.classification.predict",
		},
		Permissions: []string{"ai"},
		Count:       8,
	},
	{
		Name: "browser",
		Capabilities: []string{
			"browser.tab.list",
			"browser.tab.create",
			"browser.cookie.read",
			"browser.cookie.write",
			"browser.history.read",
		},
		Permissions: []string{"browser"},
		Count:       10,
	},
	{
		Name: "network",
		Capabilities: []string{
			"network.sniff.packet",
			"network.proxy.set",
			"network.dns.query",
			"network.http.request",
			"network.websocket.connect",
		},
		Permissions: []string{"network"},
		Count:       10,
	},
	{
		Name: "thirdparty",
		Capabilities: []string{
			"thirdparty.analytics.track",
			"thirdparty.logging.info",
			"thirdparty.metrics.collect",
			"thirdparty.alert.send",
			"thirdparty.audit.log",
		},
		Permissions: []string{"thirdparty"},
		Count:       5,
	},
}

// GenerateModules menghasilkan seluruh modul test berdasarkan kategori.
func GenerateModules() []*GeneratedModule {
	modules := make([]*GeneratedModule, 0, TotalModules())

	for _, category := range AllCategories {
		if len(category.Capabilities) == 0 || category.Count <= 0 {
			continue
		}

		for i := 0; i < category.Count; i++ {
			name := fmt.Sprintf("%s-%02d", category.Name, i+1)

			capability := category.Capabilities[i%len(category.Capabilities)]

			module := NewGeneratedModule(
				name,
				"1.0.0",
				category.Name,
				[]string{capability},
				category.Permissions,
			)

			modules = append(modules, module)
		}
	}

	return modules
}

// TotalModules mengembalikan total jumlah modul test.
func TotalModules() int {
	total := 0

	for _, category := range AllCategories {
		if category.Count > 0 {
			total += category.Count
		}
	}

	return total
}
