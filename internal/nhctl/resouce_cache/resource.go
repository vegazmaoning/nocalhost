/*
* Copyright (C) 2020 THL A29 Limited, a Tencent company.  All rights reserved.
* This source code is licensed under the Apache License Version 2.0.
 */

package resouce_cache

var GroupToTypeMap = []struct {
	K string
	V []string
}{
	{
		K: "Workloads",
		V: []string{
			"deployments",
			"dragon.io/v1alpha1/statefulsets",
			//"daemonsets",
			//"jobs",
			//"cronjobs",
			//"pods",
		},
	},
//	{
//		K: "Networks",
//		V: []string{
//			"services",
//			"endpoints",
//			"ingresses",
//			"networkpolicies",
//		},
//	},
//	{
//		K: "Configurations",
//		V: []string{
//			"configmaps",
//			"secrets",
//			"horizontalpodautoscalers",
//			"resourcequotas",
//			"poddisruptionbudgets",
//		},
//	},
//	{
//		K: "Storages",
//		V: []string{
//			"persistentvolumes",
//			"persistentvolumeclaims",
//			"storageclasses",
//		},
//	},
}
