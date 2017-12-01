package main

import (
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/mknote"
)

const (
	MaxTimeDiffMinutes = 20
)

// GPSInfo gps info of a photo
type GPSInfo struct {
	latitude  float64
	longitude float64
}

// PhotoDateGPS object holds the photo information
type PhotoDateGPS struct {
	Filename string
	Path     string
	DateTime time.Time
	GPSInfo  GPSInfo
}

// PhotoList a list of photos object, can be sorted
type PhotoList []PhotoDateGPS

func (p PhotoList) Len() int {
	return len(p)
}

func (p PhotoList) Less(i, j int) bool {
	return p[i].DateTime.Before(p[j].DateTime)
}

func (p PhotoList) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

func main() {
	args := os.Args[1:]

	paths := []string{"."}

	if len(args) > 0 {
		paths = args[:]
	}

	log.Printf("scanning photos in: %s", strings.Join(paths, ","))
	var photoWithGPS PhotoList
	var photoWithoutGPS PhotoList

	for _, path := range paths {
		gpsList, noGpsList := scanPhotosInPath(path)
		photoWithGPS = append(photoWithGPS, gpsList...)
		photoWithoutGPS = append(photoWithoutGPS, noGpsList...)
	}

	sort.Sort(photoWithGPS)
	sort.Sort(photoWithoutGPS)

	log.Printf("found %d gps tagged photos, and %d without gps tag", len(photoWithGPS), len(photoWithoutGPS))
	log.Printf("match non-gps photos to gps-photos")
	for _, photo := range photoWithoutGPS {
		nearest, diff := findNearestPhoto(photoWithGPS, photo)
		log.Printf("found nearest match for %s is %s, time diff: %s", photo.Path, nearest.Path, diff.String())
		if diff.Minutes() > MaxTimeDiffMinutes {
			log.Printf("time difference to nearest photo is too big, skip this one")
			continue
		}
		copyGPSData(nearest, photo)
	}
}

func scanPhotosInPath(folder string) (photoWithGPS, photoWithoutGPS PhotoList) {
	filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			filename := info.Name()
			if strings.HasSuffix(strings.ToLower(filename), ".jpg") ||
				strings.HasSuffix(strings.ToLower(filename), ".jpeg") ||
				strings.HasSuffix(strings.ToLower(filename), ".nef") {
				log.Printf("checking photo file: %s", path)
				photo, hasGPS, err := readPhotoFile(path)
				if err != nil {
					log.Printf("failed to read photo: %s", path)
					return nil
				}
				photo.Filename = filename
				if hasGPS {
					log.Printf("got photo data: time: %s, gps: %t, lat: %f, long: %f",
						photo.DateTime, hasGPS, photo.GPSInfo.latitude, photo.GPSInfo.longitude)

					photoWithGPS = append(photoWithGPS, photo)
				} else {
					log.Printf("got photo data: time: %s, gps: %t",
						photo.DateTime, hasGPS)

					photoWithoutGPS = append(photoWithoutGPS, photo)
				}
			} else {
				log.Printf("skip non-photo file: %s", path)
			}

		}
		return nil
	})
	return
}

func readPhotoFile(path string) (result PhotoDateGPS, hasGPS bool, err error) {
	f, err := os.Open(path)
	defer func() {
		err := f.Close()
		if err != nil {
			log.Printf("failed to close file: %v", err)
		}
	}()

	if err != nil {
		log.Printf("failed to read the photo file, error: %v", err)
		return
	}
	result.Path = path
	exif.RegisterParsers(mknote.All...)

	x, err := exif.Decode(f)
	tm, err := x.DateTime()
	if err != nil {
		log.Printf("failed to got photo create time")
		//it's useless if we can not get the creation time of a photo
		return
	}

	result.DateTime = tm

	lat, long, gpsErr := x.LatLong()
	log.Printf("%f, %f", lat, long)
	if gpsErr != nil {
		log.Printf("could not get GPS data from this photo, err: %v", gpsErr)
		hasGPS = false
	} else {
		result.GPSInfo = GPSInfo{lat, long}
		hasGPS = true
	}
	return
}

func findNearestPhoto(sortedPhoto PhotoList, photo PhotoDateGPS) (PhotoDateGPS, time.Duration) {
	length := len(sortedPhoto)
	index := sort.Search(length, func(i int) bool {
		return sortedPhoto[i].DateTime.After(photo.DateTime) || sortedPhoto[i].DateTime.Equal(photo.DateTime)
	})

	leftDuration := time.Duration(math.MaxInt64)
	rightDuration := time.Duration(math.MaxInt64)
	var left, right PhotoDateGPS

	if index < length {
		right = sortedPhoto[index]
		rightDuration = right.DateTime.Sub(photo.DateTime)
		if index > 0 {
			left = sortedPhoto[index-1]
			leftDuration = photo.DateTime.Sub(left.DateTime)
		}
	} else {
		leftDuration = photo.DateTime.Sub(sortedPhoto[length-1].DateTime)
	}

	if leftDuration < rightDuration {
		return left, leftDuration
	}
	return right, rightDuration
}

func copyGPSData(from, to PhotoDateGPS) error {
	log.Printf("copy gps data from nearest photo")
	cmd := exec.Command("exiftool", "-tagsfromfile", from.Path,
		"-GPSLatitude", "-GPSLongitude", "-GPSLongitudeRef", "-GPSLatitudeRef", to.Path)

	err := cmd.Run()
	if err != nil {
		out, err := cmd.Output()
		if err != nil {
			log.Printf("cmd failed: %v", err)
		}
		log.Printf("failed to run exiftool to copy gps data: %s", out)
	}
	return err
}
