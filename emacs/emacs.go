package emacs

import "middleware/elemInfo"

import (
	"fmt"
	"strconv"
	"strings"
	"sort"
	"time"
	"os"
	"bufio"
	"log"
	"regexp"
	"path/filepath"
)

//const (
//    orgDirectory = "~/org/"
//)

var ElemInfoCache map[string]elemInfo.ElemInfo // Maps UUID to ElemInfo

func PullDataFromOrgFiles() error {
	// Get the user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Println("Error getting home directory:", err)
		return err
	}

	// Define the org directory path
	orgDir := filepath.Join(homeDir, "org")

	// Walk through the org directory recursively
	err = filepath.Walk(orgDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Println("Error walking directory:", err)
			return err
		}

		// Check if it's a file and ends with ".org" extension
		if !info.IsDir() && filepath.Ext(path) == ".org" {
			// TODO nocheckin :: Ignore files that contain "~".
			// TODO nocheckin :: Update your org-id-scanner utility so that it doesn't ignore the pdf folder
			fmt.Println("Processing file:", path)
			ProcessOrgFile(path) // TODO handle errors
		}

		return nil
	})

	if err != nil {
		fmt.Println("Error walking directory:", err)
		return err
	}
	return nil
}

// Stores the data from org file into ElemCache
func ProcessOrgFile(filePath string) {
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inPropertiesBlock := false
	var properties map[string]string

	// Regular expression to match property lines
	propertyRegex := regexp.MustCompile(`^\s*:(\w+):\s*(.*)$`)

	for scanner.Scan() {
		line := scanner.Text()

		// Check if the line matches the first condition
		if strings.HasPrefix(line, "*") && strings.Contains(line, ":drill:") && !strings.Contains(line, ":dismissed:") {

			// Check the next line
			if scanner.Scan() {
				nextLine := strings.TrimSpace(scanner.Text())

				// Skip if the next line contains DEADLINE: or SCHEDULED:
				if strings.Contains(nextLine, "DEADLINE:") || strings.Contains(nextLine, "SCHEDULED:") {
					continue
				}

				// Check if the next line starts with :PROPERTIES:
				if strings.HasPrefix(nextLine, ":PROPERTIES:") {
					inPropertiesBlock = true
					properties = make(map[string]string) // Initialize properties map
					continue
				}
			}
		}

		// Collect properties if in the properties block
		if inPropertiesBlock {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, ":END:") {
				inPropertiesBlock = false
				// Process collected properties here
				fmt.Println("Properties:")

				// Update the ElemInfoCache with the new ElemInfo
				ei := elemInfo.Default()
				for key, value := range properties {
					var err error
					if key == "ID" {
						ei.Uuid = value
					} else if key == "SM_PRIORITY" {
						var priority float64
						priority, err = strconv.ParseFloat(value, 64)
						ei.Priority = int(priority)
					} else if key == "SM_A_FACTOR" {
						ei.AFactor, err = strconv.ParseFloat(value, 64)
					} else if key == "SM_ELEMENT_TYPE" {
						ei.ElementType = value
						if !ei.IsElementTypeValid() {
							err = fmt.Errorf("Invalid elementType = %s\n", ei.ElementType)
						}
					} else if key == "SM_ELEMENT_TYPE" {
						ei.ElementType = value
					} else if key == "SM_LAST_REVIEW" {
						value = value[1:len(value)-1]
						ei.TopicInfo.LastReview, err = time.Parse("2006-01-02 Mon 15:04", value)
					} else if key == "SM_INTERVAL" {
						ei.TopicInfo.Interval, err = strconv.ParseFloat(value, 64)
					}
					if err != nil {
						log.Fatal("error in org file parsing ", key, " with uuid: ", ei.Uuid, ", error =", err.Error())
					}
					fmt.Printf("'%s': '%s'\n", key, value)
				}
				if len(ei.Uuid) != 0 {
					Persist(ei)
				}
				properties = nil // Clear for the next block
			} else {
				// Extract key and value from property line
				match := propertyRegex.FindStringSubmatch(line)
				if len(match) == 3 {
					key := strings.Trim(match[1], ":") // Remove leading colon from key
					properties[key] = strings.TrimSpace(match[2])
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
	}
}

func FindUuidsSorted() (uuids []string) {
	// Pull the uuids from the ElemInfoCache
	for uuid, _ := range ElemInfoCache {
		uuids = append(uuids, uuid)
	}
	sort.Strings(uuids)
    return
}

func FindElemInfo(uuid string) (ei elemInfo.ElemInfo, exists bool, err error) {
	// TODO Maybe I should make the ElemInfoCache expire. Possibly this requires being defined in the RequiresCacheUpdate function
    ei, exists = ElemInfoCache[uuid]
	if !exists {
		err = PullDataFromOrgFiles()
		if err != nil {
			err = fmt.Errorf("Error in FindElemInfo w/ calling PullDataFromOrgFiles: err: ", err)
			return
		}

		ei, exists = ElemInfoCache[uuid]
		if !exists {
			return
		}
	}
	return
}

func FindDueTopics() (dueTopics []elemInfo.ElemInfo) {
	for _, ei := range ElemInfoCache {
		if !ei.IsDismissed() && ei.IsTopic() && ei.IsDueToday() {
			dueTopics = append(dueTopics, ei)
		}
	}
	return
}

func Persist(ei elemInfo.ElemInfo) {
	ElemInfoCache[ei.Uuid] = ei
}
