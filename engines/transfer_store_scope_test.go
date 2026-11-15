package engines

import "testing"

// Item 193.3: a store-limited user moves stock only at their own stores.
func TestTransferStoreDecision(t *testing.T) {
	limited := LiveUserState{Role: "Store Manager", LocationCode: "HO", Access: combineStoreAccess("Store Manager", "HO", StoreAccessSelected, []string{"JAY"}, StoreAccessHome, nil)}
	allStores := LiveUserState{Role: "Store Manager", LocationCode: "HO", Access: combineStoreAccess("Store Manager", "HO", StoreAccessAll, nil, StoreAccessHome, nil)}
	admin := LiveUserState{Role: "Super Admin", LocationCode: "HO", Access: combineStoreAccess("Super Admin", "HO", StoreAccessHome, nil, StoreAccessHome, nil)}
	for _, tc := range []struct {
		name     string
		live     LiveUserState
		step     transferStep
		from, to string
		refused  bool
	}{
		{"create between two other stores", limited, transferStepCreate, "GUIDE-TS", "GUIDE-TS2", true},
		{"create from own store", limited, transferStepCreate, "JAY", "KOR", false},
		{"create to home store", limited, transferStepCreate, "KOR", "HO", false},
		{"create with nothing named", limited, transferStepCreate, "", "", false},
		{"dispatch from a store not theirs", limited, transferStepDispatch, "KOR", "JAY", true},
		{"dispatch from own store", limited, transferStepDispatch, "JAY", "KOR", false},
		{"receive at a store not theirs", limited, transferStepReceive, "JAY", "KOR", true},
		{"receive at own store", limited, transferStepReceive, "KOR", "JAY", false},
		{"all stores user", allStores, transferStepDispatch, "X", "Y", false},
		{"admin", admin, transferStepReceive, "X", "Y", false},
	} {
		err := transferStoreDecision(tc.live, tc.step, tc.from, tc.to)
		if (err != nil) != tc.refused {
			t.Errorf("%s: err = %v, refused want %v", tc.name, err, tc.refused)
		}
		if ve, ok := err.(*ValidationError); err != nil && (!ok || ve.Code != "STORE-0855") {
			t.Errorf("%s: want STORE-0855, got %v", tc.name, err)
		}
	}
}

func TestTransferStoreInternalRunPasses(t *testing.T) {
	for _, step := range []string{"create", "dispatch", "receive"} {
		if err := CheckTransferStoreAccess("default", "", step, "A", "B"); err != nil {
			t.Errorf("%s with no user must pass: %v", step, err)
		}
	}
}
