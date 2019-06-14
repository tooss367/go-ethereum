package params

func Enable1884(gt *GasTable, jt *Jumptable) {
	// Gas cost changes
	gt.Set(Balance, 700) // Increase to 700
	gt.Set(Sload, 800)   // Increase to 800
	// Define 'SELFBALANCE'
	jt[SELFBALANCE] = operation{
		execute:     opSelfBalance,
		constantGas: GasFastStep,
		minStack:    minStack(0, 1),
		maxStack:    maxStack(0, 1),
		valid:       true,
	}
}
