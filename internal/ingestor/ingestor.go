package ingestor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

const (
	TargetURL            = "https://kplc.co.ke/customer-support"
	BaseURL              = "https://kplc.co.ke"
	LinkSelector         = "#powerschedule a"
	PdfExtension         = ".pdf"
	MaxAgeHours          = 24 * 30
	DateFormat           = "02.01.2006"
	FileDateFormat       = "02-01-06"
	OutputDir            = "out"
	OutFileFormat        = "out/%s.json"
	GeminiModel          = "gemini-flash-latest"
	GenAIPromptMessage   = "Extract blackout details"
	NominatimURLTemplate = "https://nominatim.openstreetmap.org/search?format=json&limit=1&countrycodes=ke"
	UserAgentName        = "github.com/oyamo/blackoutd-bot/1.0"

	systemPrompt = `You are an expert GIS Data Extraction AI. Scan the provided PDF of power maintenance schedules and extract the blackout details strictly according to the following JSON schema. 
ALL FIELDS ARE MANDATORY. If actual coordinates are not in the document, you MUST estimate the [longitude, latitude] based on your prior geographical knowledge of the localities in Kenya.

Schema:
[
  {
    "region": "string (Must be a real location, cleaned of any extraneous words like 'Region' or 'Area', 'Part of', etc.)",
    "county": "string (Must be a real location, cleaned of any extraneous words like 'Region' or 'Area', 'Part of', etc.)",
    "area": "string (Must be a real location, cleaned of any extraneous words like 'Region' or 'Area', 'Part of', etc.)",
    "date": "string",
    "time": "string",
    "detailed": [
      {
        "location": "string (Must be a real location, cleaned of any extraneous words like 'Region' or 'Area', 'Part of', etc.)",
        "type": "string",
        "coordinates": [
           {
             "lat": number,
             "long": number,
             "source": "string (Must ALWAYS be the literal string 'Gemini')"
           }
        ]
      }
    ]
  }
]

Do not return any markdown wrapping. Return ONLY the raw JSON array.`
)

type Coordinate struct {
	Lat    float64 `json:"lat"`
	Long   float64 `json:"long"`
	Source string  `json:"source"`
}

type DetailedLocation struct {
	Location    string       `json:"location"`
	Type        string       `json:"type"`
	Coordinates []Coordinate `json:"coordinates"`
}

type BlackoutNotice struct {
	Region   string             `json:"region"`
	County   string             `json:"county"`
	Area     string             `json:"area"`
	Date     string             `json:"date"`
	Time     string             `json:"time"`
	Detailed []DetailedLocation `json:"detailed"`
}

type NominatimResult struct {
	Lat string `json:"lat"`
	Lon string `json:"lon"`
}

func Start(apiKey string) {
	runIngestion(apiKey)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		slog.Info("Running scheduled ingestion")
		runIngestion(apiKey)
	}
}

func runIngestion(apiKey string) {
	slog.Info("Starting initial ingestion")

	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		slog.Error("Failed to create genai client", "err", err)
		return
	}
	defer client.Close()

	res, err := http.Get(TargetURL)
	if err != nil {
		slog.Error("Failed fetching customer-support page", "err", err)
		return
	}
	defer res.Body.Close()

	doc, err := goquery.NewDocumentFromReader(res.Body)
	if err != nil {
		slog.Error("Failed to read HTML", "err", err)
		return
	}

	dateRegex := regexp.MustCompile(`(\d{2})\.(\d{2})\.(\d{4})`)
	now := time.Now()
	os.MkdirAll(OutputDir, 0755)

	doc.Find(LinkSelector).Each(func(_ int, s *goquery.Selection) {
		link, exists := s.Attr("href")
		if !exists || !strings.HasSuffix(strings.ToLower(link), PdfExtension) {
			return
		}
		if !strings.HasPrefix(link, "http") {
			link = BaseURL + link
		}

		matches := dateRegex.FindStringSubmatch(s.Text())
		if len(matches) < 4 {
			return
		}

		t, err := time.Parse(DateFormat, matches[0])
		if err != nil || now.Sub(t).Hours() > MaxAgeHours {
			return
		}

		if err := processPDF(ctx, client, link, t); err != nil {
			slog.Error("Error processing PDF", "link", link, "err", err)
		}
	})
}

func processPDF(ctx context.Context, client *genai.Client, link string, t time.Time) error {
	outPath := fmt.Sprintf(OutFileFormat, t.Format(FileDateFormat))

	if _, err := os.Stat(outPath); err == nil {
		return nil
	}

	slog.Info("Processing new PDF link", "link", link, "date", t.Format(FileDateFormat))

	res, err := http.Get(link)
	if err != nil {
		return fmt.Errorf("failed fetching PDF: %w", err)
	}
	defer res.Body.Close()

	tmpFile := filepath.Join(os.TempDir(), filepath.Base(link))
	f, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("failed creating tmp file: %w", err)
	}
	io.Copy(f, res.Body)
	f.Close()
	defer os.Remove(tmpFile)

	fUpload, err := os.Open(tmpFile)
	if err != nil {
		return fmt.Errorf("failed opening tmp file for upload: %w", err)
	}
	defer fUpload.Close()

	gFile, err := client.UploadFile(ctx, "", fUpload, nil)
	if err != nil {
		return fmt.Errorf("upload err: %w", err)
	}
	defer client.DeleteFile(ctx, gFile.Name)

	model := client.GenerativeModel(GeminiModel)
	model.SystemInstruction = &genai.Content{Parts: []genai.Part{genai.Text(systemPrompt)}}
	model.ResponseMIMEType = "application/json"

	resp, err := model.GenerateContent(ctx, genai.FileData{URI: gFile.URI}, genai.Text(GenAIPromptMessage))
	if err != nil {
		return fmt.Errorf("LLM generation err: %w", err)
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return fmt.Errorf("LLM returned empty or malformed output")
	}

	text, ok := resp.Candidates[0].Content.Parts[0].(genai.Text)
	if !ok {
		return fmt.Errorf("LLM parts cast failed")
	}

	rawJSON := strings.TrimSpace(strings.Trim(string(text), "` \njson"))

	var notices []BlackoutNotice
	if err := json.Unmarshal([]byte(rawJSON), &notices); err != nil {
		os.WriteFile(outPath, []byte(rawJSON), 0644)
		slog.Warn("Failed to unmarshal LLM format, raw output saved", "outPath", outPath, "err", err)
		return nil
	}

	rateLimiter := time.NewTicker(1 * time.Second)
	defer rateLimiter.Stop()

	for idxNotice, notice := range notices {
		for idxDetail, detail := range notice.Detailed {
			apiURL := buildNominatimURL(detail, notice)
			if apiURL == "" {
				continue
			}

			<-rateLimiter.C

			req, err := http.NewRequest("GET", apiURL, nil)
			if err != nil {
				continue
			}

			req.Header.Set("User-Agent", UserAgentName)
			osResp, err := http.DefaultClient.Do(req)
			if err != nil {
				continue
			}

			var nomResults []NominatimResult
			if err := json.NewDecoder(osResp.Body).Decode(&nomResults); err != nil || len(nomResults) == 0 {
				osResp.Body.Close()
				continue
			}

			var coords []Coordinate
			for _, result := range nomResults {
				lat, _ := strconv.ParseFloat(result.Lat, 64)
				lon, _ := strconv.ParseFloat(result.Lon, 64)
				if lat != 0 && lon != 0 {
					coords = append(coords, Coordinate{Lat: lat, Long: lon, Source: "OSM"})
				}
			}
			if len(coords) > 0 {
				notices[idxNotice].Detailed[idxDetail].Coordinates = coords
			}
			osResp.Body.Close()
		}
	}

	finalBytes, _ := json.MarshalIndent(notices, "", "  ")
	os.WriteFile(outPath, finalBytes, 0644)
	slog.Info("Successfully saved parsed data", "outPath", outPath)

	return nil
}

func buildNominatimURL(detail DetailedLocation, notice BlackoutNotice) string {
	if detail.Location == "" {
		return ""
	}

	u, _ := url.Parse(NominatimURLTemplate)
	q := u.Query()
	parts := []string{detail.Location, notice.Area, notice.County, notice.Region, "Kenya"}
	var filtered []string
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	q.Set("q", strings.Join(filtered, ", "))
	u.RawQuery = q.Encode()
	return u.String()
}
