package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

// Stage 42.4.4 - Cartonization v2: dimensional (volume) and weight-aware,
// alongside SuggestCartonization's original qty-capacity-only first-fit-
// decreasing (engines/wms_pack_count.go, untouched by this file). Reuses
// Item.weight/Item.volume, the same two fields 42.2.6's enforceBinCapacity
// already reads, and 42.4's own migration's max_weight_capacity/
// max_volume_capacity on CartonType. An item or carton type that never sets
// these values simply contributes 0, so a tenant that only ever used
// max_qty_capacity gets a v2 result identical to v1's.

// CartonizationItemV2 is one SKU/qty line to be packed, same shape as
// CartonizationItem (wms_pack_count.go) plus nothing extra - weight/volume
// are looked up server-side from Item, not supplied by the caller, so a
// caller cannot understate a line's true footprint.
type CartonizationItemV2 = CartonizationItem

// itemPhysicals is one item's per-unit weight/volume, looked up once per
// call rather than per box placement.
type itemPhysicals struct {
	weight, volume float64
}

// SuggestCartonizationV2 (42.4.4) packs items into cartonTypeCode boxes
// first-fit-decreasing by volume (falling back to qty when no item in the
// batch has a volume set, so behaviour degrades to v1's own ordering rather
// than an arbitrary one), refusing to place a unit into a box that would
// exceed ANY of the three configured ceilings (qty, weight, volume) that
// carton type actually has set. A single unit that alone exceeds a
// configured ceiling is refused outright, the same "cannot ever be packed"
// class of error SuggestCartonization already raises for capacity <= 0.
func SuggestCartonizationV2(tenantID, cartonTypeCode string, items []CartonizationItemV2) ([]SuggestedCarton, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one item line is required")
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return nil, err
	}
	maxQty, maxWeight, maxVolume, err := getCartonCapacityV2(schema, cartonTypeCode)
	if err != nil {
		return nil, err
	}
	if maxQty <= 0 {
		return nil, fmt.Errorf("carton type %s has no usable max_qty_capacity configured", cartonTypeCode)
	}

	sorted := make([]CartonizationItemV2, len(items))
	copy(sorted, items)
	for i := range sorted {
		if sorted[i].UOM == "" {
			continue
		}
		eaches, err := ConvertUOMQty(tenantID, sorted[i].Sku, float64(sorted[i].Qty), sorted[i].UOM, "EA")
		if err != nil {
			return nil, err
		}
		sorted[i].Qty = int(eaches)
		sorted[i].UOM = ""
	}

	physicals := map[string]itemPhysicals{}
	for _, it := range sorted {
		if _, ok := physicals[it.Sku]; ok || it.Sku == "" {
			continue
		}
		w, v := itemWeightVolume(schema, it.Sku)
		physicals[it.Sku] = itemPhysicals{weight: w, volume: v}
		if maxWeight > 0 && w > maxWeight {
			return nil, fmt.Errorf("item %s (weight %.2f) alone exceeds carton type %s's max_weight_capacity of %.2f", it.Sku, w, cartonTypeCode, maxWeight)
		}
		if maxVolume > 0 && v > maxVolume {
			return nil, fmt.Errorf("item %s (volume %.2f) alone exceeds carton type %s's max_volume_capacity of %.2f", it.Sku, v, cartonTypeCode, maxVolume)
		}
	}

	sort.Slice(sorted, func(i, j int) bool {
		vi, vj := physicals[sorted[i].Sku].volume, physicals[sorted[j].Sku].volume
		if vi != vj {
			return vi > vj
		}
		return sorted[i].Qty > sorted[j].Qty
	})

	var boxes []SuggestedCarton
	type boxLoad struct {
		qty            int
		weight, volume float64
	}
	var loads []boxLoad
	for _, it := range sorted {
		if it.Sku == "" || it.Qty <= 0 {
			continue
		}
		phys := physicals[it.Sku]
		remaining := it.Qty
		for remaining > 0 {
			placedIdx := -1
			for bi := range boxes {
				roomQty := maxQty - loads[bi].qty
				if roomQty <= 0 {
					continue
				}
				if maxWeight > 0 && loads[bi].weight+phys.weight > maxWeight {
					continue
				}
				if maxVolume > 0 && loads[bi].volume+phys.volume > maxVolume {
					continue
				}
				placedIdx = bi
				break
			}
			if placedIdx == -1 {
				boxes = append(boxes, SuggestedCarton{BoxID: fmt.Sprintf("BOX%d", len(boxes)+1), CartonType: cartonTypeCode, MaxCapacity: maxQty})
				loads = append(loads, boxLoad{})
				placedIdx = len(boxes) - 1
			}
			take := remaining
			if room := maxQty - loads[placedIdx].qty; take > room {
				take = room
			}
			if maxWeight > 0 && phys.weight > 0 {
				if maxRoom := int((maxWeight - loads[placedIdx].weight) / phys.weight); take > maxRoom {
					if maxRoom < 1 {
						maxRoom = 1
					}
					take = maxRoom
				}
			}
			if maxVolume > 0 && phys.volume > 0 {
				if maxRoom := int((maxVolume - loads[placedIdx].volume) / phys.volume); take > maxRoom {
					if maxRoom < 1 {
						maxRoom = 1
					}
					take = maxRoom
				}
			}
			if take <= 0 {
				take = 1 // a single unit is always placed, even snug against a ceiling
			}
			boxes[placedIdx].Items = append(boxes[placedIdx].Items, CartonizationItem{Sku: it.Sku, Qty: take})
			boxes[placedIdx].UsedCapacity += take
			loads[placedIdx].qty += take
			loads[placedIdx].weight += float64(take) * phys.weight
			loads[placedIdx].volume += float64(take) * phys.volume
			remaining -= take
		}
	}
	return boxes, nil
}

func getCartonCapacityV2(schema, cartonTypeCode string) (maxQty int, maxWeight, maxVolume float64, err error) {
	var dataStr string
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'CartonType' AND (id = $1 OR data->>'code' = $1) AND status = 'Active'`, schema),
		cartonTypeCode).Scan(&dataStr)
	if err == sql.ErrNoRows {
		return 0, 0, 0, fmt.Errorf("no Active CartonType %q found", cartonTypeCode)
	} else if err != nil {
		return 0, 0, 0, err
	}
	var data map[string]interface{}
	if uerr := json.Unmarshal([]byte(dataStr), &data); uerr != nil {
		return 0, 0, 0, uerr
	}
	return int(numFromInterface(data["max_qty_capacity"])), numFromInterface(data["max_weight_capacity"]), numFromInterface(data["max_volume_capacity"]), nil
}

func itemWeightVolume(schema, sku string) (weight, volume float64) {
	var dataStr string
	if err := db.DB.QueryRow(fmt.Sprintf(
		`SELECT data FROM %s.documents WHERE doctype = 'Item' AND data->>'code' = $1`, schema), sku).Scan(&dataStr); err != nil {
		return 0, 0
	}
	var data map[string]interface{}
	_ = json.Unmarshal([]byte(dataStr), &data)
	return numFromInterface(data["weight"]), numFromInterface(data["volume"])
}
