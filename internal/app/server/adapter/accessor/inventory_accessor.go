package accessor

import (
	"context"
	"io/fs"
	"path"
	"strings"

	"github.com/kakkky/gorviz/internal/app/server/domain"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
	"github.com/kakkky/gorviz/internal/domain/observation"
)

type InventoryAccessor struct{ root *filesystem.Root }

func NewInventoryAccessor(root *filesystem.Root) *InventoryAccessor {
	return &InventoryAccessor{root: root}
}

func dirNames(root *filesystem.Root, dir string) ([]string, error) {
	entries, err := root.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, unPathName(e.Name()))
		}
	}
	return out, nil
}

func (a *InventoryAccessor) ListServices(ctx context.Context) ([]observation.ServiceName, error) {
	names, err := dirNames(a.root, servicesDir)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "list services")
	}
	out := make([]observation.ServiceName, 0, len(names))
	for _, n := range names {
		out = append(out, observation.ServiceName(n))
	}
	return out, nil
}

func (a *InventoryAccessor) ListInstances(ctx context.Context, s observation.ServiceName) ([]observation.InstanceName, error) {
	names, err := dirNames(a.root, path.Join(servicesDir, pathName(string(s)), "instances"))
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "list instances")
	}
	out := make([]observation.InstanceName, 0, len(names))
	for _, n := range names {
		out = append(out, observation.InstanceName(n))
	}
	return out, nil
}

type DataFileAccessor struct{ root *filesystem.Root }

func NewDataFileAccessor(root *filesystem.Root) *DataFileAccessor {
	return &DataFileAccessor{root: root}
}

func (a *DataFileAccessor) size(p string) (int64, error) {
	var total int64
	err := a.root.Walk(p, func(_ string, info fs.FileInfo) error {
		total += info.Size()
		return nil
	})
	return total, err
}

func (a *DataFileAccessor) kindFiles(dir string, k domain.DataKind, s observation.ServiceName, i observation.InstanceName) ([]domain.DataFile, error) {
	entries, err := a.root.ReadDir(path.Join(dir, string(k)))
	if err != nil {
		return nil, err
	}
	var out []domain.DataFile
	for _, e := range entries {
		start, ok := parsePeriod(k, e.Name())
		if !ok {
			continue
		}
		p := path.Join(dir, string(k), e.Name())
		size, err := a.size(p)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.DataFile{Kind: k, Service: s, Instance: i, Path: p, PeriodStart: start, Size: size})
	}
	return out, nil
}

func (a *DataFileAccessor) ListDataFiles(ctx context.Context) ([]domain.DataFile, error) {
	services, err := dirNames(a.root, servicesDir)
	if err != nil {
		return nil, domain.ErrInternal.Wrap(err, "list services")
	}
	var out []domain.DataFile
	for _, sn := range services {
		s := observation.ServiceName(sn)
		for _, k := range domain.DataKinds {
			files, err := a.kindFiles(serviceDir(s), k, s, "")
			if err != nil {
				return nil, domain.ErrInternal.Wrap(err, "list data files")
			}
			out = append(out, files...)
		}
		instances, err := dirNames(a.root, path.Join(servicesDir, pathName(sn), "instances"))
		if err != nil {
			return nil, domain.ErrInternal.Wrap(err, "list instances")
		}
		for _, in := range instances {
			i := observation.InstanceName(in)
			for _, k := range domain.DataKinds {
				files, err := a.kindFiles(instanceDir(s, i), k, s, i)
				if err != nil {
					return nil, domain.ErrInternal.Wrap(err, "list data files")
				}
				out = append(out, files...)
			}
		}
	}
	return out, nil
}

func (a *DataFileAccessor) RemoveDataFiles(ctx context.Context, files []domain.DataFile) error {
	for _, f := range files {
		if !strings.HasPrefix(f.Path, servicesDir+"/") {
			continue
		}
		if err := a.root.RemoveAll(f.Path, servicesDir); err != nil {
			return domain.ErrInternal.Wrap(err, "remove a data file")
		}
	}
	return nil
}
