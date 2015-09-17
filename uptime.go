// Visualises the uptime pattern of Tor relays.

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"log"
	"math"
	"os"
	"sort"
	"sync"
	"time"

	tor "git.torproject.org/user/phw/zoossh.git"
)

const (
	tolerance   = 3
	blockLength = 5
	maxDistance = 0.0002
)

// numBits maps an 8-bit integer to the numbers of its bits.
var numBits = map[int]int{
	0: 0, 1: 1, 2: 1, 3: 2, 4: 1, 5: 2, 6: 2, 7: 3, 8: 1, 9: 2,
	10: 2, 11: 3, 12: 2, 13: 3, 14: 3, 15: 4, 16: 1, 17: 2, 18: 2, 19: 3,
	20: 2, 21: 3, 22: 3, 23: 4, 24: 2, 25: 3, 26: 3, 27: 4, 28: 3, 29: 4,
	30: 4, 31: 5, 32: 1, 33: 2, 34: 2, 35: 3, 36: 2, 37: 3, 38: 3, 39: 4,
	40: 2, 41: 3, 42: 3, 43: 4, 44: 3, 45: 4, 46: 4, 47: 5, 48: 2, 49: 3,
	50: 3, 51: 4, 52: 3, 53: 4, 54: 4, 55: 5, 56: 3, 57: 4, 58: 4, 59: 5,
	60: 4, 61: 5, 62: 5, 63: 6, 64: 1, 65: 2, 66: 2, 67: 3, 68: 2, 69: 3,
	70: 3, 71: 4, 72: 2, 73: 3, 74: 3, 75: 4, 76: 3, 77: 4, 78: 4, 79: 5,
	80: 2, 81: 3, 82: 3, 83: 4, 84: 3, 85: 4, 86: 4, 87: 5, 88: 3, 89: 4,
	90: 4, 91: 5, 92: 4, 93: 5, 94: 5, 95: 6, 96: 2, 97: 3, 98: 3, 99: 4,
	100: 3, 101: 4, 102: 4, 103: 5, 104: 3, 105: 4, 106: 4, 107: 5, 108: 4, 109: 5,
	110: 5, 111: 6, 112: 3, 113: 4, 114: 4, 115: 5, 116: 4, 117: 5, 118: 5, 119: 6,
	120: 4, 121: 5, 122: 5, 123: 6, 124: 5, 125: 6, 126: 6, 127: 7, 128: 1, 129: 2,
	130: 2, 131: 3, 132: 2, 133: 3, 134: 3, 135: 4, 136: 2, 137: 3, 138: 3, 139: 4,
	140: 3, 141: 4, 142: 4, 143: 5, 144: 2, 145: 3, 146: 3, 147: 4, 148: 3, 149: 4,
	150: 4, 151: 5, 152: 3, 153: 4, 154: 4, 155: 5, 156: 4, 157: 5, 158: 5, 159: 6,
	160: 2, 161: 3, 162: 3, 163: 4, 164: 3, 165: 4, 166: 4, 167: 5, 168: 3, 169: 4,
	170: 4, 171: 5, 172: 4, 173: 5, 174: 5, 175: 6, 176: 3, 177: 4, 178: 4, 179: 5,
	180: 4, 181: 5, 182: 5, 183: 6, 184: 4, 185: 5, 186: 5, 187: 6, 188: 5, 189: 6,
	190: 6, 191: 7, 192: 2, 193: 3, 194: 3, 195: 4, 196: 3, 197: 4, 198: 4, 199: 5,
	200: 3, 201: 4, 202: 4, 203: 5, 204: 4, 205: 5, 206: 5, 207: 6, 208: 3, 209: 4,
	210: 4, 211: 5, 212: 4, 213: 5, 214: 5, 215: 6, 216: 4, 217: 5, 218: 5, 219: 6,
	220: 5, 221: 6, 222: 6, 223: 7, 224: 3, 225: 4, 226: 4, 227: 5, 228: 4, 229: 5,
	230: 5, 231: 6, 232: 4, 233: 5, 234: 5, 235: 6, 236: 5, 237: 6, 238: 6, 239: 7,
	240: 4, 241: 5, 242: 5, 243: 6, 244: 5, 245: 6, 246: 6, 247: 7, 248: 5, 249: 6,
	250: 6, 251: 7, 252: 6, 253: 7, 254: 7, 255: 8,
}

// Highlights stores which columns in the resulting image should be
// highlighted.
type Highlights map[int]bool

// Day represents the uptime/downtime pattern of a relay for a single day.
type Day uint32

// MarkOnline marks a given hour in the day as online, i.e., it sets the bit
// position to 1.
func (day *Day) MarkOnline(hour uint) {

	*day = Day(uint32(*day) | (1 << hour))
}

// IsOnline returns true if the relay was online at the given hour.
func (day *Day) IsOnline(hour uint32) bool {

	return (uint32(*day) & (1 << hour)) > 0
}

// OnlineSequence represents a sequence of days.
type OnlineSequence []Day

// AddDay adds a day to the online sequence.
func (seq *OnlineSequence) AddDay() {

	*seq = append(*seq, Day(0))
}

// TotalUptime counts the number of hours, the relay was online.
func (seq *OnlineSequence) TotalUptime() int {

	total := 0
	for _, day := range *seq {
		byte1 := numBits[(int(day)&0x000000ff)>>0]
		byte2 := numBits[(int(day)&0x0000ff00)>>8]
		byte3 := numBits[(int(day)&0x00ff0000)>>16]
		byte4 := numBits[(int(day)&0xff000000)>>24]

		total += (byte1 + byte2 + byte3 + byte4)
	}

	return total
}

// Median determines the median of the given online sequence.
func (seq *OnlineSequence) Median() float32 {

	var hour uint32
	indices := make([]uint32, 0)

	for i, day := range *seq {
		for hour = 0; hour < 24; hour++ {
			if day.IsOnline(hour) {
				indices = append(indices, uint32(i)+hour)
			}
		}
	}

	indicesLen := len(indices)
	if indicesLen == 0 {
		log.Fatalln("Length of indices for calculation of median must not be zero.  Bug?")
	} else if indicesLen == 1 {
		return float32(indices[0])
	}

	if (indicesLen % 2) == 0 {
		idx := indicesLen / 2
		return float32(indices[idx-1]+indices[idx]) / 2
	} else {
		idx := int(math.Ceil(float64(indicesLen) / 2))
		return float32(indices[idx])
	}
}

// OrderedUptimes is used to sort columns in the picture.
type OrderedUptimes struct {
	Fingerprints []tor.Fingerprint
	Sequences    []OnlineSequence
}

// Len implements the sort interface.
func (ou OrderedUptimes) Len() int {

	return len(ou.Fingerprints)
}

// Swap implements the sort interface.
func (ou OrderedUptimes) Swap(i, j int) {

	ou.Sequences[i], ou.Sequences[j] = ou.Sequences[j], ou.Sequences[i]
	ou.Fingerprints[i], ou.Fingerprints[j] = ou.Fingerprints[j], ou.Fingerprints[i]
}

// Less implements the sort interface.
func (ou OrderedUptimes) Less(i, j int) bool {

	total1 := ou.Sequences[i].TotalUptime()
	total2 := ou.Sequences[j].TotalUptime()

	diff := total1 - total2
	if (diff > -tolerance) && (diff < tolerance) {
		median1 := ou.Sequences[i].Median()
		median2 := ou.Sequences[j].Median()
		return median1 < median2
	} else {
		return total1 < total2
	}
}

// Uptimes maps relay fingerprints to their online sequence.
type Uptimes struct {
	ForFingerprint map[tor.Fingerprint]OnlineSequence
}

// AddDay adds a day to all relays in the map.
func (up *Uptimes) AddDay() {

	counter := 0
	for fpr, seq := range up.ForFingerprint {
		seq = append(seq, Day(0))
		up.ForFingerprint[fpr] = seq
		counter++
	}
}

// UptimeDistance determines the distance between two online sequences.
func UptimeDistance(seq1, seq2 OnlineSequence) (float32, error) {

	var distance, boost float32
	var hour uint32

	if len(seq1) != len(seq2) {
		return 0, fmt.Errorf("Both sequences must have same length.\n")
	}

	for day, _ := range seq1 {
		for hour = 0; hour < 24; hour++ {
			status1 := seq1[day].IsOnline(hour)
			status2 := seq2[day].IsOnline(hour)

			// Relays don't have same status: increase distance and boost.
			if status1 != status2 {
				if boost < 1 {
					boost += 0.1
				}
				distance += boost
			} else {
				boost = 0
			}
		}
	}

	return distance / float32(len(seq1)*24), nil
}

// SortUptimes sorts uptime sequences, so uptimes that are visually similar are
// close to each other.
func SortUptimes(uptimes *Uptimes) *OrderedUptimes {

	start := time.Now()

	ordered := &OrderedUptimes{
		Fingerprints: make([]tor.Fingerprint, 0),
		Sequences:    make([]OnlineSequence, 0),
	}

	for fpr, seq := range uptimes.ForFingerprint {
		ordered.Fingerprints = append(ordered.Fingerprints, fpr)
		ordered.Sequences = append(ordered.Sequences, seq)
	}

	sort.Sort(ordered)

	log.Printf("Done sorting list after %s.\n", time.Since(start))

	return ordered
}

// GetHighlights attempts to highlight columns that are suspiciously similar.
// The highlight is meant as a visual aide to find Sybils in the resulting
// image.  Two columns are highlighted if their uptime distance is smaller than
// the given threshold.
func GetHighlights(uptimes *OrderedUptimes) *Highlights {

	highlight := Highlights{}
	runlength := 0
	hours := len(uptimes.Sequences[0]) * 24

	// Determine distance between subsequent relay columns.
	for i := 0; i < len(uptimes.Fingerprints)-1; i++ {

		timeOnline := uptimes.Sequences[i].TotalUptime()
		if timeOnline < 5 || (hours-timeOnline) < 5 {
			continue
		}

		distance, _ := UptimeDistance(uptimes.Sequences[i], uptimes.Sequences[i+1])
		if distance < maxDistance {
			runlength++
		} else {
			if runlength >= blockLength {
				for x := 0; x >= -runlength; x-- {
					highlight[i+x] = true
				}
			}
			runlength = 0
		}
	}

	return &highlight
}

// PruneUptimes gets rid of columns that are of little interest, i.e., relays
// that are mostly online.
func PruneUptimes(uptimes *Uptimes) {

	var mostlyOnline, maxOnline, prevRelays int
	prevRelays = len(uptimes.ForFingerprint)

	for fpr, seq := range uptimes.ForFingerprint {
		if maxOnline == 0 {
			maxOnline = len(seq) * 24
		}

		if seq.TotalUptime() == maxOnline {
			mostlyOnline++
			delete(uptimes.ForFingerprint, fpr)
		}
	}

	log.Printf("Pruned %d (out of %d) relays that were mostly online.\n", mostlyOnline, prevRelays)
}

// AnalyseUptimes analyses the uptime pattern of Tor relays and generates an
// image, that should help with finding Sybils.
func AnalyseUptimes(channel chan tor.ObjectSet, params *CmdLineParams, group *sync.WaitGroup) {

	defer group.Done()

	uptimes := Uptimes{
		ForFingerprint: make(map[tor.Fingerprint]OnlineSequence),
	}
	hour := -1
	daysPassed := -1

	// One loop iteration corresponds to one consensus.
	for objects := range channel {

		hour = (hour + 1) % 24
		if hour == 0 {
			daysPassed++
			uptimes.AddDay()
		}

		// Iterate over all relays in the consensus.
		for object := range objects.Iterate() {

			fpr := object.GetFingerprint()
			daySeq, exists := uptimes.ForFingerprint[fpr]
			if !exists {
				daySeq = make(OnlineSequence, daysPassed+1)
				uptimes.ForFingerprint[fpr] = daySeq
			}

			last := len(daySeq) - 1
			daySeq[last].MarkOnline(uint(hour))
		}
	}

	if len(uptimes.ForFingerprint) == 0 {
		log.Fatalln("No consensuses to process.  Exiting.")
	}

	PruneUptimes(&uptimes)

	sortedUptimes := SortUptimes(&uptimes)
	GenImage(sortedUptimes, GetHighlights(sortedUptimes), params.InputData)
}

// GenImage generates an images out of the generated uptime pattern.  Columns
// that are suspiciously similar are highlighted.
func GenImage(uptimes *OrderedUptimes, highlight *Highlights, fileName string) {

	// x-axis: relay fingerprints, y-axis: uptime sequences.
	x := len(uptimes.Fingerprints)
	y := len(uptimes.Sequences[0]) * 24

	img := image.NewRGBA(image.Rect(0, 0, x, y))
	offline := color.RGBA{255, 255, 255, 255}
	online := color.RGBA{0, 0, 0, 255}
	important := color.RGBA{255, 0, 0, 255}

	log.Printf("Generating %dx%d uptime image.\n", x, y)

	j := 0
	var hour uint32
	for x, _ := range uptimes.Fingerprints {
		for y, day := range uptimes.Sequences[x] {
			for hour = 0; hour < 24; hour++ {
				if day.IsOnline(hour) {
					if _, exists := (*highlight)[x]; exists {
						img.Set(x, (y*24)+int(hour), important)
					} else {
						img.Set(x, (y*24)+int(hour), online)
					}
				} else {
					img.Set(x, (y*24)+int(hour), offline)
				}
			}
		}
		j++
	}

	fd, err := os.Create(fileName)
	if err != nil {
		log.Fatal(err)
	}

	err = jpeg.Encode(fd, img, &jpeg.Options{Quality: 100})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Wrote image file to: %s\n", fileName)
}