// Package image keeps the images of watched containers up to date.
//
// It checks registries for newer images and pulls them, within the image
// cooldown and through any registry mirrors. It also builds images from Git
// contexts and removes images that are no longer used.
package image
