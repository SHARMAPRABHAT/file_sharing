package main

import (
	"reflect"
	"testing"
)

func TestDeliveryLinksWithWhitespaceSeparatedSheetURLs(t *testing.T) {
	raw := "https://drive.google.com/file/d/first/view?usp=drive_link " +
		"https://drive.google.com/file/d/second/view?usp=drive_link  " +
		"https://drive.google.com/file/d/third/view?usp=drive_link"
	parts := splitDriveLinks(raw)

	content := Content{
		Title: "Commerce Dictionary",
	}
	for _, part := range parts {
		content.GoogleDriveIDs = append(content.GoogleDriveIDs, GoogleDriveLink{ID: part})
	}

	got := deliveryLinks(content)
	wantURLs := []string{
		"https://drive.google.com/file/d/first/view?usp=drive_link",
		"https://drive.google.com/file/d/second/view?usp=drive_link",
		"https://drive.google.com/file/d/third/view?usp=drive_link",
	}
	wantTitles := []string{
		"Commerce Dictionary - PDF 1",
		"Commerce Dictionary - PDF 2",
		"Commerce Dictionary - PDF 3",
	}
	if len(got) != len(wantURLs) {
		t.Fatalf("deliveryLinks returned %d links, want %d", len(got), len(wantURLs))
	}

	gotURLs := make([]string, len(got))
	for i, link := range got {
		gotURLs[i] = link.URL
		if link.Title != wantTitles[i] {
			t.Errorf("link %d title = %q, want %q", i+1, link.Title, wantTitles[i])
		}
	}
	if !reflect.DeepEqual(gotURLs, wantURLs) {
		t.Errorf("deliveryLinks URLs = %#v, want %#v", gotURLs, wantURLs)
	}
}

func TestSplitDriveLinksSupportsPipeDelimiter(t *testing.T) {
	got := splitDriveLinks("first|second | third")
	want := []string{"first", "second", "third"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitDriveLinks() = %#v, want %#v", got, want)
	}
}
