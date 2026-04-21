package frontendassets

import "io/fs"

func Select(assetFS fs.FS) (fs.FS, error) {
	if distAssets, err := fs.Sub(assetFS, "frontend/dist"); err == nil {
		if _, err := fs.Stat(distAssets, "index.html"); err == nil {
			return distAssets, nil
		}
	}

	placeholderAssets, err := fs.Sub(assetFS, "frontend")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(placeholderAssets, "index.html"); err != nil {
		return nil, err
	}
	return placeholderAssets, nil
}
