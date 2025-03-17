//go:build js && wasm

package main

import (
	"fmt"
	"math"
	"syscall/js"
	// "os"
	// "runtime/pprof"
)

var coilTypes = map[string]CoilData{
	// {efficiency, bonus, extractionRate}
	"Iron":         {0.33, 1, 0.1},
	"Copper":       {0.396, 1, 0.12},
	"Osmium":       {0.462, 1, 0.12},
	"Steel":        {0.495, 1, 0.13},
	"Invar":        {0.495, 1, 0.14},
	"Silver":       {0.561, 1, 0.15},
	"Gold":         {0.66, 1, 0.175},
	"Electrum":     {0.825, 1, 0.2},
	"Platinum":     {0.99, 1, 0.25},
	"Enderium":     {0.99, 1.02, 0.3},
	"Ludicrite":    {1.15, 1.02, 0.35},
	"AllTheModium": {1.2, 1.02, 0.4},
	"Vibranium":    {1.35, 1.04, 0.5},
	"Unobtanium":   {1.5, 1.06, 0.7},
}

const minHeight int = 4
const minWidth int = 5

type FlowSettingVariant int64

const (
	UseMaxFlow FlowSettingVariant = iota
	FindBestFlow
	UseSetFlow
	FindBestUnderFlow
)

type FlowSetting struct {
	variant FlowSettingVariant
	value   int64
}

func findOptimalTurbine(fitnessFunction func(Turbine) float64, constraintsFunction func(Turbine) bool, coilType CoilData, flowSetting FlowSetting, maxSize Size) Turbine {
	var bestTurbine Turbine
	bestFitness := math.Inf(-1)

	for height := minHeight; height <= int(maxSize.y); height++ {
		for width := minWidth; width <= int(maxSize.x); width += 2 {
			for coilLayers := 1; coilLayers <= height-3; coilLayers++ {
				turbine, err := NewTurbine(int32(height), int32(width), int32(coilLayers), coilType)
				if err != nil {
					fmt.Println(err.Error())
					fmt.Printf("Couldn't form a valid turbine %d %d %d\n", height, width, coilLayers)
					continue
				}

				if !constraintsFunction(turbine) {
					continue
				}

				flowRates := []int64{}
				switch flowSetting.variant {
				case UseMaxFlow:
					flowRates = append(flowRates, turbine.maxMaxFlowRate)
				case FindBestFlow:
					for flowRate := flowSetting.value; flowRate <= turbine.maxMaxFlowRate; flowRate += flowSetting.value {
						flowRates = append(flowRates, int64(flowRate))
					}
				case UseSetFlow:
					flowRates = append(flowRates, flowSetting.value)
				case FindBestUnderFlow:
					for flowRate := max(0, flowSetting.value-10000); flowRate <= min(turbine.maxMaxFlowRate, flowSetting.value); flowRate += 100 {
						flowRates = append(flowRates, int64(flowRate))
					}
				default:
					panic("Invalid FlowSettingVariant")
				}

				for _, flowRate := range flowRates {
					// set the rate to test
					turbine.SetNominalFlowRate(flowRate)

					// calculate the rpm from the closed form
					calculatedRPM := turbine.FinalRPM()
					// set the final energy for the final rpm
					turbine.SetEnergyForRPM(calculatedRPM)
					// tick the turbine to get all the bonus data
					turbine.Tick()

					// evaluate the turbine with the provided fitness function
					turbineFitness := fitnessFunction(turbine)

					if turbineFitness > bestFitness {
						// turbine.PrintStats()
						bestTurbine = turbine
						bestFitness = turbineFitness
					}
				}
			}
		}
	}

	return bestTurbine
}

func optimizerWrapper() js.Func {
	jsonFunc := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) != 1 {
			return "Invalid no of arguments passed"
		}

		coilMaterial := args[0].String()
		coilType := coilTypes[coilMaterial]
		fitnessFunction := func(turbine Turbine) float64 {
			return turbine.energyGeneratedLastTick
		}
		constraintsFunction := func(turbine Turbine) bool {
			return true
		}
		maxSize := Size{10, 50, 10}

		turbine := findOptimalTurbine(fitnessFunction, constraintsFunction, coilType, FlowSetting{UseMaxFlow, 0}, maxSize)

		turbine.PrintStats()
		turbine.PrintBuildCost()

		return turbine
	})

	return jsonFunc

}

func main() {
	js.Global().Set("runOptimizer", optimizerWrapper())
	<-make(chan struct{})
}
