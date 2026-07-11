// Mock Database for Custom ERP Master Data

const INITIAL_ERP_DATA = {
  brands: [
    { id: 'b1', code: 'BRD01', name: 'BRD01', status: 'Active' },
    { id: 'b2', code: 'NKS', name: 'NKS', status: 'Active' },
    { id: 'b3', code: 'A3', name: 'A3', status: 'Active' },
    { id: 'b4', code: 'AAQM', name: 'AAQM', status: 'Active' },
    { id: 'b5', code: 'KALDAN', name: 'KALDAN', status: 'Active' },
    { id: 'b6', code: 'R2D', name: 'R2D', status: 'Active' },
    { id: 'b7', code: '3AS', name: '3AS', status: 'Active' },
    { id: 'b8', code: 'SSM', name: 'SSM', status: 'Active' },
    { id: 'b9', code: 'DID', name: 'DID', status: 'Active' },
    { id: 'b10', code: 'BM3', name: 'BM3', status: 'Active' }
  ],
  subBrands: [
    { id: 'sb1', code: 'SBRD01', brandId: 'b2', name: 'Nike Air', status: 'Active' },
    { id: 'sb2', code: 'SBRD02', brandId: 'b2', name: 'Nike Pro', status: 'Active' }
  ],
  styles: [
    { id: 'st1', code: 'STY01', name: 'Modern', status: 'Active' },
    { id: 'st2', code: 'STY02', name: 'Classic', status: 'Active' },
    { id: 'st3', code: 'STY03', name: 'Vintage', status: 'Active' }
  ],
  subStyles: [
    { id: 'sst1', code: 'SST01', styleId: 'st1', name: 'Matte Modern', status: 'Active' },
    { id: 'sst2', code: 'SST02', styleId: 'st2', name: 'Glossy Classic', status: 'Active' }
  ],
  productCategories: [
    { id: 'pc1', code: 'CAT01', name: 'GOLD JEWELLERY', isWeight: true, isNetWeight: true, status: 'Active' },
    { id: 'pc2', code: 'CAT02', name: 'SILVER JEWELLERY', isWeight: true, isNetWeight: false, status: 'Active' },
    { id: 'pc3', code: 'CAT03', name: 'DIAMOND JEWELLERY', isWeight: false, isNetWeight: false, status: 'Active' }
  ],
  productTypes: [
    { id: 'pt1', code: 'TYP01', name: 'BAJUBAND', categoryId: 'pc1', description: 'Traditional armlet', status: 'Active' },
    { id: 'pt2', code: 'TYP02', name: 'BANGLES', categoryId: 'pc1', description: 'Traditional bangles', status: 'Active' },
    { id: 'pt3', code: 'TYP03', name: 'EARRING', categoryId: 'pc2', description: 'Silver earrings', status: 'Active' }
  ],
  itemNames: [
    { id: 'in1', code: 'ITM01', categoryId: 'pc1', productTypeId: 'pt1', hsnCode: '7113', stickerType: 'Standard', name: 'GOLD JEWELLERY BAJUBAND', status: 'Active' },
    { id: 'in2', code: 'ITM02', categoryId: 'pc1', productTypeId: 'pt2', hsnCode: '7113', stickerType: 'Thermal', name: 'GOLD JEWELLERY BANGLES', status: 'Active' }
  ],
  colors: [
    { id: 'c1', code: 'COL01', name: 'Yellow Gold', status: 'Active' },
    { id: 'c2', code: 'COL02', name: 'Rose Gold', status: 'Active' },
    { id: 'c3', code: 'COL03', name: 'Silver White', status: 'Active' }
  ],
  secondaryColors: [
    { id: 'sc1', code: 'SCOL01', name: 'Light Red', status: 'Active' },
    { id: 'sc2', code: 'SCOL02', name: 'Dark Blue', status: 'Active' }
  ],
  fabricColors: [
    { id: 'fc1', code: 'FCOL01', name: 'Velvet Red', status: 'Active' }
  ],
  polishes: [
    { id: 'p1', code: 'POL01', name: 'High Gloss', status: 'Active' },
    { id: 'p2', code: 'POL02', name: 'Matte Finish', status: 'Active' }
  ]
};

// Export to window or module system
if (typeof window !== 'undefined') {
  window.INITIAL_ERP_DATA = INITIAL_ERP_DATA;
}
