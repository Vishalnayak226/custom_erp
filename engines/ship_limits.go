package engines

import (
	"custom_erp/db"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/lib/pq"
)

// Stage 146.4.4 - ship-from-store limits.
//
// applyShipLimits narrows the locations that can cover an order by what each
// Location says about itself: which channels it serves (ship_channels), an
// online-orders switch (ship_online), a daily order cap (ship_daily_cap), a
// distance limit from the PIN master, and a priority (ship_priority). Each of
// the first four is hard (the location is out) or soft (it ranks below every
// location that breaks fewer rules), set per rule in OMS settings. A location
// with none of these set, and a tenant with no PIN master, route exactly as
// before. Only ResolveAllocationPlan passes a scope; the channel-fixed
// location path and the returns routing call qualifyingLocations unscoped.

// sourceScope is the order a routing decision is for.
type sourceScope struct {
	channel  string
	orderPin string
}

func newSourceScope(channel, shippingAddress string) *sourceScope {
	return &sourceScope{channel: strings.TrimSpace(channel), orderPin: pincodeShapeRe.FindString(shippingAddress)}
}

type shipLimitRow struct {
	channels string
	online   string
	capStr   string
	prioStr  string
	pin      string
}

func shipRuleIsHard(schema, key string) bool {
	return GetSettingStringForSchema(schema, key) != "soft"
}

func applyShipLimits(schema string, cands []candidateLocation, sc *sourceScope) ([]candidateLocation, error) {
	if sc == nil || len(cands) == 0 {
		return cands, nil
	}
	codes := make([]string, len(cands))
	for i, c := range cands {
		codes[i] = c.code
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT id, COALESCE(data->>'code', ''), COALESCE(data->>'ship_channels', ''), COALESCE(data->>'ship_online', ''),
		       COALESCE(data->>'ship_daily_cap', ''), COALESCE(data->>'ship_priority', ''), COALESCE(data->>'pincode', '')
		  FROM %s.documents
		 WHERE doctype = 'Location' AND deleted_at IS NULL AND (id = ANY($1) OR data->>'code' = ANY($1))`, schema), pq.Array(codes))
	if err != nil {
		return nil, err
	}
	info := map[string]shipLimitRow{}
	for rows.Next() {
		var id, code string
		var r shipLimitRow
		if err := rows.Scan(&id, &code, &r.channels, &r.online, &r.capStr, &r.prioStr, &r.pin); err != nil {
			rows.Close()
			return nil, err
		}
		info[id] = r
		if code != "" {
			info[code] = r
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	maxKm := GetSettingIntForSchema(schema, "oms.ship_max_distance_km")
	var capCodes []string
	for _, c := range cands {
		if cp, _ := strconv.Atoi(strings.TrimSpace(info[c.code].capStr)); cp > 0 {
			capCodes = append(capCodes, c.code)
		}
	}
	today, err := ordersSentToday(schema, capCodes)
	if err != nil {
		return nil, err
	}
	var coords map[string][2]float64
	if maxKm > 0 && sc.orderPin != "" {
		pins := []string{sc.orderPin}
		for _, c := range cands {
			if p := strings.TrimSpace(info[c.code].pin); p != "" {
				pins = append(pins, p)
			}
		}
		if coords, err = pinCoordinates(schema, pins); err != nil {
			return nil, err
		}
	}

	hardChannel := shipRuleIsHard(schema, "oms.ship_rule_channel")
	hardOnline := shipRuleIsHard(schema, "oms.ship_rule_online")
	hardCap := shipRuleIsHard(schema, "oms.ship_rule_cap")
	hardDist := shipRuleIsHard(schema, "oms.ship_rule_distance")

	type ranked struct {
		c    candidateLocation
		tier int
		prio int
	}
	var kept []ranked
	minTier := math.MaxInt32
	for _, c := range cands {
		r := info[c.code]
		tier, out := 0, false
		breach := func(hard bool) {
			if hard {
				out = true
			} else {
				tier++
			}
		}
		if list := strings.TrimSpace(r.channels); list != "" && sc.channel != "" {
			served := false
			for _, ch := range strings.Split(list, ",") {
				if strings.EqualFold(strings.TrimSpace(ch), sc.channel) {
					served = true
					break
				}
			}
			if !served {
				breach(hardChannel)
			}
		}
		if strings.EqualFold(strings.TrimSpace(r.online), "No") {
			breach(hardOnline)
		}
		if cp, _ := strconv.Atoi(strings.TrimSpace(r.capStr)); cp > 0 && today[c.code] >= cp {
			breach(hardCap)
		}
		if maxKm > 0 {
			if km, ok := pinDistanceKm(coords, sc.orderPin, r.pin); ok && km > float64(maxKm) {
				breach(hardDist)
			}
		}
		if out {
			continue
		}
		prio := math.MaxInt32
		if p, err := strconv.Atoi(strings.TrimSpace(r.prioStr)); err == nil {
			prio = p
		}
		kept = append(kept, ranked{c, tier, prio})
		if tier < minTier {
			minTier = tier
		}
	}
	minPrio := math.MaxInt32
	for _, k := range kept {
		if k.tier == minTier && k.prio < minPrio {
			minPrio = k.prio
		}
	}
	var out []candidateLocation
	for _, k := range kept {
		if k.tier == minTier && k.prio == minPrio {
			out = append(out, k.c)
		}
	}
	return out, nil
}

// ordersSentToday counts the distinct orders routed to each location since the
// business day began (cancelled lines do not count).
func ordersSentToday(schema string, codes []string) (map[string]int, error) {
	out := map[string]int{}
	if len(codes) == 0 {
		return out, nil
	}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'location_code', COUNT(DISTINCT data->>'order_id')
		  FROM %s.documents
		 WHERE doctype = 'SalesOrderLine' AND deleted_at IS NULL AND status <> 'Cancelled'
		   AND data->>'location_code' = ANY($1) AND created_at >= public.erp_day_start(public.erp_today())
		 GROUP BY 1`, schema), pq.Array(codes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		var n int
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		out[code] = n
	}
	return out, rows.Err()
}

// pinCoordinates reads latitude / longitude for the PINs the admin has
// imported into the PinCode master. A PIN not in it is simply absent.
func pinCoordinates(schema string, pins []string) (map[string][2]float64, error) {
	out := map[string][2]float64{}
	rows, err := db.DB.Query(fmt.Sprintf(`
		SELECT data->>'code', data->>'latitude', data->>'longitude'
		  FROM %s.documents
		 WHERE doctype = 'PinCode' AND status = 'Active' AND deleted_at IS NULL AND data->>'code' = ANY($1)`, schema), pq.Array(pins))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code, lat, lon string
		if err := rows.Scan(&code, &lat, &lon); err != nil {
			return nil, err
		}
		la, e1 := strconv.ParseFloat(strings.TrimSpace(lat), 64)
		lo, e2 := strconv.ParseFloat(strings.TrimSpace(lon), 64)
		if e1 == nil && e2 == nil {
			out[code] = [2]float64{la, lo}
		}
	}
	return out, rows.Err()
}

// pinDistanceKm is the great-circle distance between two PINs, ok=false when
// either is missing from the master.
func pinDistanceKm(coords map[string][2]float64, a, b string) (float64, bool) {
	pa, okA := coords[strings.TrimSpace(a)]
	pb, okB := coords[strings.TrimSpace(b)]
	if !okA || !okB {
		return 0, false
	}
	rad := math.Pi / 180
	dLat, dLon := (pb[0]-pa[0])*rad, (pb[1]-pa[1])*rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(pa[0]*rad)*math.Cos(pb[0]*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 6371 * 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h)), true
}
