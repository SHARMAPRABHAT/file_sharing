package main

import (
	"reflect"
	"strings"
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

func TestParseCategoriesCSVIncludesOnlyEnabledCategories(t *testing.T) {
	input := `Category ID,Title,Description,Status
commerce-dictionary,COMMERCE DICTIONARY,Commerce terms,Enabled
commerce-special-notes,COMMERCE SPECIAL NOTES,Special revision notes,Disabled
paper-1-pyqs-and-practices,PAPER 1 PYQS AND PRACTICES,Paper 1 practice,enabled
`
	got, err := parseCategoriesCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseCategoriesCSV() error = %v", err)
	}
	want := []Category{
		{ID: "commerce-dictionary", Title: "COMMERCE DICTIONARY", Description: "Commerce terms"},
		{ID: "paper-1-pyqs-and-practices", Title: "PAPER 1 PYQS AND PRACTICES", Description: "Paper 1 practice"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseCategoriesCSV() = %#v, want %#v", got, want)
	}
}

func TestParseCourseCatalogCSVUsesSheetColumns(t *testing.T) {
	input := `pack,title,price,discount_price,early_bird_price,offer_price,discount_percent,sold,total,offer_title,description,guide_url,google_drive_id,status
course-b,Course B,"₹10,000","₹1,299",₹999,₹999,90,3,50,Offer B,Course description,https://example.com/guide-b,https://drive.google.com/file/d/delivery-b/view,Enable
course-hidden,Hidden Course,1000,800,700,700,30,1,10,Hidden Offer,Hidden description,,https://drive.google.com/file/d/hidden/view,Disabled
course-a,Course A,5000,2000,1500,1500,70,4,25,Offer A,Second course,,https://drive.google.com/file/d/delivery-a/view,Enabled
`
	got, err := parseCourseCatalogCSV(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseCourseCatalogCSV() error = %v", err)
	}

	wantOrder := []string{"course-b", "course-a"}
	if !reflect.DeepEqual(got.Order, wantOrder) {
		t.Errorf("course order = %#v, want %#v", got.Order, wantOrder)
	}
	if len(got.Courses) != len(wantOrder) {
		t.Fatalf("loaded %d courses, want %d", len(got.Courses), len(wantOrder))
	}
	wantCourse := CourseOfferState{
		Title:           "Course B",
		Description:     "Course description",
		OfferTitle:      "Offer B",
		Price:           10000,
		DiscountPrice:   1299,
		EarlyBirdPrice:  999,
		OfferPrice:      999,
		DiscountPercent: 90,
		Total:           50,
		Sold:            3,
		GuideURL:        "https://example.com/guide-b",
		GoogleDriveID:   "https://drive.google.com/file/d/delivery-b/view",
	}
	if !reflect.DeepEqual(got.Courses["course-b"], wantCourse) {
		t.Errorf("course-b = %#v, want %#v", got.Courses["course-b"], wantCourse)
	}
}
