package collectionresources

import (
	"context"
	"errors"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metainternal "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/apiserver/pkg/registry/rest"
	"k8s.io/klog/v2"

	internal "github.com/clusterpedia-io/clusterpedia/pkg/apis/clusterpedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/apis/clusterpedia/scheme"
	"github.com/clusterpedia-io/clusterpedia/pkg/apis/clusterpedia/v1beta1"
	"github.com/clusterpedia-io/clusterpedia/pkg/kubeapiserver/storageconfig"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils/request"
)

type REST struct {
	list     *internal.CollectionResourceList
	storages map[string]storage.CollectionResourceStorage
}

var _ rest.Lister = &REST{}
var _ rest.Scoper = &REST{}
var _ rest.Getter = &REST{}

func NewREST(factory storage.StorageFactory) *REST {
	crs, err := factory.GetCollectionResources(context.TODO())
	if err != nil {
		klog.Fatal(err)
	}

	list := &internal.CollectionResourceList{}
	storages := make(map[string]storage.CollectionResourceStorage, len(crs))
	configFactory := storageconfig.NewStorageConfigFactory()
	for _, cr := range crs {
		for irt := range cr.ResourceTypes {
			rt := &cr.ResourceTypes[irt]
			config, err := configFactory.NewConfig(rt.GroupResource().WithVersion(""))
			if err != nil {
				continue
			}

			*rt = internal.CollectionResourceType{
				Group:    config.StorageGroupResource.Group,
				Version:  config.StorageVersion.Version,
				Resource: config.StorageGroupResource.Resource,
			}
		}

		storage, err := factory.NewCollectionResourceStorage(cr)
		if err != nil {
			continue
		}
		storages[cr.Name] = storage
		list.Items = append(list.Items, *cr)
	}

	return &REST{list, storages}
}

func (s *REST) New() runtime.Object {
	return &internal.CollectionResource{}
}

func (s *REST) NewList() runtime.Object {
	return &internal.CollectionResourceList{}
}

func (s *REST) NamespaceScoped() bool {
	return false
}

func (s *REST) List(ctx context.Context, options *metainternal.ListOptions) (runtime.Object, error) {
	return s.list, nil
}

func (s *REST) Get(ctx context.Context, name string, _ *metav1.GetOptions) (runtime.Object, error) {
	var opts internal.ListOptions
	query := request.RequestQueryFrom(ctx)
	if err := scheme.ParameterCodec.DecodeParameters(query, v1beta1.SchemeGroupVersion, &opts); err != nil {
		return nil, err
	}

	// collection resources don't support with remaining count
	// ignore opts.WithRemainingCount
	opts.WithRemainingCount = nil

	storage, ok := s.storages[name]
	if !ok {
		return nil, errors.New("")
	}
	return storage.Get(ctx, &opts)
}

func (s *REST) ConvertToTable(ctx context.Context, object runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	resourceColumnDefinition := []metav1.TableColumnDefinition{
		{Name: "Cluster", Type: "string"},
		{Name: "Group", Type: "string"},
		{Name: "Version", Type: "string"},
		{Name: "Kind", Type: "string"},
		{Name: "Namespace", Type: "string"},
		{Name: "Name", Type: "string", Format: "name"},
		{Name: "Age", Type: "string"},
	}

	listColumnDefinition := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string"},
		{Name: "Resources", Type: "string"},
	}

	table := &metav1.Table{}
	switch obj := object.(type) {
	case *internal.CollectionResource:
		var rows []metav1.TableRow
		for _, obj := range obj.Items {
			m, err := meta.Accessor(obj)
			if err != nil {
				return nil, err
			}

			timestrap := "<unknown>"
			t := m.GetCreationTimestamp()
			if !t.IsZero() {
				timestrap = duration.HumanDuration(time.Since(m.GetCreationTimestamp().Time))
			}

			gvk := obj.GetObjectKind().GroupVersionKind()
			cluster := utils.ExtractClusterName(obj)
			rows = append(rows, metav1.TableRow{
				Object: runtime.RawExtension{Object: obj},
				Cells:  []interface{}{cluster, gvk.Group, gvk.Version, gvk.Kind, m.GetNamespace(), m.GetName(), timestrap},
			})
		}

		table.Rows = rows
		table.ColumnDefinitions = resourceColumnDefinition
	case *internal.CollectionResourceList:
		var rows []metav1.TableRow
		for _, item := range obj.Items {
			name := item.Name
			var grs []string
			for _, rt := range item.ResourceTypes {
				grs = append(grs, rt.GroupResource().String())
			}

			rows = append(rows, metav1.TableRow{
				Object: runtime.RawExtension{Object: item.DeepCopy()},
				Cells:  []interface{}{name, strings.Join(grs, ",")},
			})
		}

		table.Rows = rows
		table.ColumnDefinitions = listColumnDefinition
	}
	return table, nil
}
