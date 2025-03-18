package main

import (
	"errors"
	"fmt"
	"math"
)

type Size struct {
	x, y, z int32
}

type Vec4 struct {
	w, x, y, z int32
}

type CoilData struct {
	efficiency, bonus, extractionRate float64
}

type VentState int64

const (
	VentStateOverflow VentState = iota
	VentStateAll
	VentStateClosed
)

type Turbine struct {
	// inner size of the turbine
	size Size

	active bool

	coilSize int64

	inductionEfficiency          float64
	inductorDragCoefficient      float64
	inductionEnergyExponentBonus float64

	rotorCapacityPerRPM float64

	maxFlowRate    int64
	maxMaxFlowRate int64

	rotorShafts int32

	rotorAxialMass                 float64
	rotorMass                      float64
	linearBladeMetersPerRevolution float64

	coilEngaged bool

	rotorEnergy       float64
	fluidTankCapacity float64
	batteryCapacity   float64

	energyGeneratedLastTick float64
	rotorEfficiencyLastTick float64

	inductorDragLastTick   float64
	frictionDragLastTick   float64
	aeroDragLastTick       float64
	coilEfficiencyLastTick float64
}

// TODO config
const FlowRatePerBlock int64 = 5000
const LatentHeat float64 = 4.0
const TurbineMultiplier float64 = 2.5
const FluidPerBladeLinerKilometre float64 = 20.0
const RotorAxialMassPerShaft float64 = 100.0
const RotorAxialMassPerBlade float64 = 100.0
const CoilDragMultiplier float64 = 10.0
const BatterySizePerCoilBlock float64 = 300000
const TankVolumePerBlock float64 = 10000
const EffectiveGridFrequency float64 = 30
const EfficiencyPeaks float64 = 2
const FrictionDragMultiplier float64 = 5.0e-4
const AerodynamicDragMultiplier float64 = 5.0e-4

var log2 float64 = math.Log(2)
var logPeakRPM float64 = math.Log(EffectiveGridFrequency * 60)
var MinEfficiencyScale float64 = math.Pow(2, EfficiencyPeaks-0.5)

func NewTurbine(height, width, coilLayers int32, coilType CoilData) (Turbine, error) {
	turbine := Turbine{}

	if width%2 == 0 {
		return turbine, errors.New("Turbine width must be odd")
	}
	if coilLayers > height-3 {
		return turbine, errors.New("Turbine cannot hold that many coil layers")
	}
	if height < 4 || width < 5 {
		return turbine, errors.New("Turbine cannot be this small")
	}
	if coilLayers < 1 {
		return turbine, errors.New("Turbine needs at least one coil layer")
	}

	// internal dimensions of the turbine
	turbineDimensions := Size{width - 2, height - 2, width - 2}

	turbine.Reset()
	turbine.Resize(turbineDimensions)

	turbine.SetFullCoil(coilLayers, coilType)

	rotors := []Vec4{}
	for range turbineDimensions.y - int32(coilLayers) {
		bladeLength := turbineDimensions.x / 2
		rotors = append(rotors, Vec4{bladeLength, bladeLength, bladeLength, bladeLength})
	}
	for range coilLayers {
		rotors = append(rotors, Vec4{})
	}
	turbine.SetRotorConfiguration(rotors)

	turbine.UpdateInternalValues()

	turbine.active = true
	turbine.coilEngaged = true
	turbine.SetNominalFlowRate(0)

	return turbine, nil
}

func (turbine *Turbine) Reset() {
	turbine.rotorEnergy = 0.0
}

func (turbine *Turbine) Resize(dim Size) {
	turbine.size = dim
	turbine.coilSize = 0

	turbine.inductionEfficiency = 0
	turbine.inductorDragCoefficient = 0
	turbine.inductionEnergyExponentBonus = 0

	turbine.maxMaxFlowRate = (int64(turbine.size.x)*int64(turbine.size.z) - 1 /* bearing*/) * FlowRatePerBlock
}

func (turbine *Turbine) SetNominalFlowRate(flowRate int64) {
	turbine.maxFlowRate = min(turbine.maxMaxFlowRate, max(0, flowRate))
}

func (turbine *Turbine) SetRotorConfiguration(rotorConfiguration []Vec4) {
	turbine.rotorMass = 0
	turbine.linearBladeMetersPerRevolution = 0

	for _, bladeLevel := range rotorConfiguration {
		sumRangeFromZero := func(x int32) int64 { return int64(x+1) * int64(x) / 2 }
		turbine.linearBladeMetersPerRevolution += float64(sumRangeFromZero(bladeLevel.w))
		turbine.linearBladeMetersPerRevolution += float64(sumRangeFromZero(bladeLevel.x))
		turbine.linearBladeMetersPerRevolution += float64(sumRangeFromZero(bladeLevel.y))
		turbine.linearBladeMetersPerRevolution += float64(sumRangeFromZero(bladeLevel.z))
		turbine.rotorMass += float64(bladeLevel.w + bladeLevel.x + bladeLevel.y + bladeLevel.z)
	}

	turbine.rotorCapacityPerRPM = turbine.linearBladeMetersPerRevolution * FluidPerBladeLinerKilometre
	turbine.rotorCapacityPerRPM /= 1000
	turbine.rotorCapacityPerRPM *= 2 * math.Pi

	turbine.rotorShafts = int32(len(rotorConfiguration))

	turbine.rotorAxialMass = float64(turbine.rotorShafts) * RotorAxialMassPerShaft
	turbine.rotorAxialMass += turbine.linearBladeMetersPerRevolution * RotorAxialMassPerBlade

	turbine.rotorMass *= RotorAxialMassPerBlade
	turbine.rotorMass += float64(turbine.rotorShafts) * RotorAxialMassPerShaft

	if turbine.maxFlowRate == -1 {
		turbine.SetNominalFlowRate(int64(turbine.rotorCapacityPerRPM * 1800))
	}
}

func (turbine *Turbine) SetCoilData(x, y int32, coilData CoilData) {
	turbine.inductionEfficiency += coilData.efficiency
	turbine.inductionEnergyExponentBonus += coilData.bonus

	distance := max(math.Abs(float64(x)), math.Abs(float64(y)))

	layerMultiplier := func(distance float64) float64 {
		if distance < 1 {
			return 1
		} else {
			return 2 / (distance + 1)
		}
	}
	turbine.inductorDragCoefficient += coilData.extractionRate * layerMultiplier(distance)
	turbine.coilSize++
}

func (turbine *Turbine) SetFullCoil(layerNumber int32, coilData CoilData) {
	for i := range turbine.size.x / 2 {
		// fmt.Print(i, " ")
		coilsOnLayer := float64((i+1)*2*4) * float64(layerNumber)
		turbine.coilSize += int64(coilsOnLayer)
		turbine.inductionEfficiency += coilData.efficiency * coilsOnLayer
		turbine.inductionEnergyExponentBonus += coilData.bonus * coilsOnLayer
		turbine.inductorDragCoefficient += coilData.extractionRate * coilsOnLayer * (2.0 / (float64(i) + 2.0))
	}
}

func (turbine *Turbine) UpdateInternalValues() {
	turbine.inductorDragCoefficient *= CoilDragMultiplier

	turbine.batteryCapacity = float64(turbine.coilSize+1) * BatterySizePerCoilBlock

	if turbine.coilSize <= 0 {
		turbine.inductionEfficiency = 0
		turbine.inductorDragCoefficient = 0
		turbine.inductionEnergyExponentBonus = 0
	} else {
		turbine.inductionEfficiency /= float64(turbine.coilSize)
		turbine.inductionEnergyExponentBonus /= float64(turbine.coilSize)
		turbine.inductorDragCoefficient /= float64(turbine.coilSize)
	}

	turbine.fluidTankCapacity = (float64(turbine.size.x)*float64(turbine.size.y)*float64(turbine.size.z) - (float64(turbine.rotorShafts) + float64(turbine.coilSize))) * TankVolumePerBlock
}

func (turbine *Turbine) RPM() float64 {
	return turbine.rotorEnergy / turbine.rotorAxialMass
}

func fasterPow(x, y float64) float64 {
	if x == 0 || y == 1 {
		return x
	}

	// a = x
	// x = y
	// a + a (x - 1) log(a) + 1/2 a (x - 1)^2 log^2(a) + 1/6 a (x - 1)^3 log^3(a) + 1/24 a (x - 1)^4 log^4(a) + 1/120 a (x - 1)^5 log^5(a) + O((x - 1)^6)
	// (Taylor series)
	// (converges everywhere)

	// 5 terms give relative error of about 1.8e-5 which should be acceptable for 7.5x speedup

	// v1 1440ms
	// return x + x*p + 0.5*x*p*p + 1.0/6.0*x*p*p*p + 1.0/24.0*x*p*p*p*p
	// v2 750ms
	// Don't question, it's just 2x faster...
	p := (y - 1) * math.Log(x)
	r := x
	ret := x
	r *= p
	ret += r
	r *= 0.5 * p
	ret += r
	r *= 0.33333333333 * p
	ret += r
	r *= 0.25 * p
	ret += r
	return ret
}

func (turbine *Turbine) Tick() {
	rpm := turbine.RPM()

	if turbine.active {
		flowRate := float64(turbine.maxFlowRate)
		effectiveFlowRate := flowRate

		rotorCapacity := turbine.rotorCapacityPerRPM * max(100, rpm)

		if flowRate > rotorCapacity {
			excessFlow := flowRate - rotorCapacity
			excessEfficiency := rotorCapacity / flowRate
			effectiveFlowRate = rotorCapacity + excessFlow*excessEfficiency
		}

		if flowRate != 0 {
			turbine.rotorEfficiencyLastTick = effectiveFlowRate / flowRate
		} else {
			turbine.rotorEfficiencyLastTick = 0
		}

		if effectiveFlowRate > 0 {
			turbine.rotorEnergy += effectiveFlowRate * LatentHeat * TurbineMultiplier
		}
	} else {
		turbine.rotorEfficiencyLastTick = 0
	}

	if turbine.coilEngaged {
		inductionTorque := rpm * turbine.inductorDragCoefficient * float64(turbine.coilSize)
		energyToGenerate := fasterPow(inductionTorque, turbine.inductionEnergyExponentBonus) * turbine.inductionEfficiency

		var efficiency float64

		frequency := EffectiveGridFrequency
		peakRPM := frequency * 60
		minRPM := peakRPM / MinEfficiencyScale
		if rpm < minRPM {
			efficiency = 0.5
		} else if rpm > peakRPM {
			numerator := -(rpm - peakRPM) * (rpm - peakRPM)
			denominator := 8 * frequency * peakRPM
			possibleEfficiency := numerator / denominator
			efficiency = max(0, possibleEfficiency+1)
		} else {
			logValue := -2*((math.Log(rpm)-logPeakRPM)/log2) + 1
			efficiency = -0.25*math.Cos(logValue*math.Pi) + 0.75
		}
		turbine.coilEfficiencyLastTick = efficiency

		energyToGenerate *= efficiency

		turbine.energyGeneratedLastTick = energyToGenerate

		turbine.inductorDragLastTick = inductionTorque
		turbine.rotorEnergy -= inductionTorque
	} else {
		turbine.inductorDragLastTick = 0
		turbine.energyGeneratedLastTick = 0
	}

	turbine.frictionDragLastTick = turbine.rotorMass * (rpm * FrictionDragMultiplier) * (rpm * FrictionDragMultiplier)
	turbine.rotorEnergy -= turbine.frictionDragLastTick
	turbine.aeroDragLastTick = turbine.linearBladeMetersPerRevolution * (rpm * AerodynamicDragMultiplier) * (rpm * AerodynamicDragMultiplier)
	turbine.rotorEnergy -= turbine.aeroDragLastTick

	if turbine.rotorEnergy < 0 {
		turbine.rotorEnergy = 0
	}
}

func (turbine Turbine) FinalRPM() float64 {
	flowRate := float64(turbine.maxFlowRate)
	RFPerHeat := LatentHeat * TurbineMultiplier

	// first assume that we have: final rpm < 100
	effectiveFlowRate := flowRate
	rotorCapacity := turbine.rotorCapacityPerRPM * 100
	if flowRate > rotorCapacity {
		// excessFlow := flowRate - rotorCapacity
		// excessEfficiency := rotorCapacity / flowRate
		// effectiveFlowRate = rotorCapacity + excessFlow*excessEfficiency
		effectiveFlowRate = rotorCapacity + rotorCapacity - rotorCapacity*rotorCapacity/flowRate
	}

	a := turbine.rotorMass*FrictionDragMultiplier*FrictionDragMultiplier + turbine.linearBladeMetersPerRevolution*AerodynamicDragMultiplier*AerodynamicDragMultiplier
	b := turbine.inductorDragCoefficient * float64(turbine.coilSize)
	c := -effectiveFlowRate * RFPerHeat

	predictedRPM := (-b + math.Sqrt(b*b-4*a*c)) / (2 * a)

	if predictedRPM < 100 {
		// fmt.Println(a, b, c)
		// fmt.Printf("Low speed %.2f\n", predictedRPM)
		return predictedRPM
	}

	// now assume we have no restriction on flow
	c = -flowRate * RFPerHeat

	predictedRPM = (-b + math.Sqrt(b*b-4*a*c)) / (2 * a)
	predictedRotorCapacity := turbine.rotorCapacityPerRPM * predictedRPM

	if flowRate < predictedRotorCapacity {
		// we indeed have enough capacity
		// fmt.Println("Enough capacity")
		return predictedRPM
	}

	// we have capacity issues so we take the third path

	a += turbine.rotorCapacityPerRPM * turbine.rotorCapacityPerRPM / flowRate * RFPerHeat
	b += -2 * turbine.rotorCapacityPerRPM * RFPerHeat
	// c = 0

	predictedRPM = -b / a

	// fmt.Printf("Not enough capacity %.2f\n", predictedRPM)
	return predictedRPM
}

func (turbine *Turbine) SetEnergyForRPM(rpm float64) {
	turbine.rotorEnergy = turbine.rotorAxialMass * rpm
}

func (turbine Turbine) PrintStats() {
	coilLayers := turbine.coilSize / (int64(turbine.size.x)*int64(turbine.size.z) - 1)
	fmt.Printf("\nHeight %d, Width %d, Coil layers: %d\n", turbine.size.y+2, turbine.size.x+2, coilLayers)
	fmt.Printf("Producing %.1f RF/t\n", turbine.energyGeneratedLastTick)
	fmt.Printf("Current flow: %dmb/t; Current rpm: %.1f\n", turbine.maxFlowRate, turbine.RPM())
	fmt.Printf("Current rotor capacity: %.1fmb/t\n", turbine.rotorCapacityPerRPM*turbine.RPM())
	fmt.Printf("%.3f RF/mb; Rotor flow efficiency: %.1f%%\n", turbine.energyGeneratedLastTick/float64(turbine.maxFlowRate), turbine.rotorEfficiencyLastTick*100)
	fmt.Printf("Coil efficiency at rpm: %.1f%%\n", turbine.coilEfficiencyLastTick*100)
	totalDrag := turbine.frictionDragLastTick + turbine.aeroDragLastTick + turbine.inductorDragLastTick
	usedDrag := turbine.inductorDragLastTick
	fmt.Printf("Drag experienced = friction: %.1f; aero: %.1f; coil : %.1f\n", turbine.frictionDragLastTick, turbine.aeroDragLastTick, turbine.inductorDragLastTick)
	fmt.Printf("Useful drag: %f%%\n\n", usedDrag/totalDrag*100)
}

func (turbine Turbine) PrintBuildCost() {
	fmt.Printf("1 Turbine Controller\n1 Turbine Power Tap\n2 Tubine IO Ports\n2 Turbine Bearings\n")
	fmt.Printf("%d Turbine Casings\n", 4*(turbine.size.x+turbine.size.y+turbine.size.z)-16)
	fmt.Printf("%d Turbine Glass\n", 2*((turbine.size.x-2)*(turbine.size.y-2)+(turbine.size.x-2)*(turbine.size.z-2)+(turbine.size.y-2)*(turbine.size.z-2))-6)
	fmt.Printf("%d Coil Blocks\n", turbine.coilSize)
	fmt.Printf("%d Shafts\n", turbine.rotorShafts)
	rotorBlades := int((turbine.rotorMass - (float64(turbine.rotorShafts) * RotorAxialMassPerShaft)) / RotorAxialMassPerBlade)
	fmt.Printf("%d Rotor Blades\n", rotorBlades)
}
