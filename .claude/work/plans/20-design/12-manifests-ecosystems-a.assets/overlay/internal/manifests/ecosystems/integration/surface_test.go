package integration_test

import (
	"github.com/cantara/source-code-portal/internal/manifests"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/cargo"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/golang"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/gradle"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/maven"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/npm"
	"github.com/cantara/source-code-portal/internal/manifests/ecosystems/python"
)

// The exported surface of the six packages, asserted at compile time exactly as the design document (section 2.2) states it.
var (
	_ func(*manifests.Registry) error                                          = golang.Register
	_ func(*manifests.Registry) error                                          = npm.Register
	_ func(*manifests.Registry) error                                          = python.Register
	_ func(*manifests.Registry, ...maven.Option) error                         = maven.Register
	_ func(*manifests.Registry) error                                          = gradle.Register
	_ func(*manifests.Registry) error                                          = cargo.Register
	_ func() manifests.EcosystemSpec                                           = golang.Spec
	_ func() manifests.EcosystemSpec                                           = npm.Spec
	_ func() manifests.EcosystemSpec                                           = python.Spec
	_ func() manifests.EcosystemSpec                                           = maven.Spec
	_ func() manifests.EcosystemSpec                                           = gradle.Spec
	_ func() manifests.EcosystemSpec                                           = cargo.Spec
	_ func(...maven.Option) manifests.Assembler                                = maven.NewAssembler
	_ func(manifests.PomSource, func(manifests.ComponentID) bool) maven.Option = maven.WithPomSource
	_ func() manifests.Assembler                                               = golang.NewAssembler
	_ func() manifests.Assembler                                               = npm.NewAssembler
	_ func() manifests.Assembler                                               = python.NewAssembler
	_ func() manifests.Assembler                                               = gradle.NewAssembler
	_ func() manifests.Assembler                                               = cargo.NewAssembler
	_ func(string) string                                                      = python.NormalizeName
	_ func(string) (python.Requirement, bool)                                  = python.ParseRequirement
	_ func(string) (string, bool)                                              = python.ExactPin
	_ manifests.EcosystemID                                                    = gradle.ID
	_ manifests.Parser                                                         = golang.NewModParser()
	_ manifests.Kinded                                                         = golang.NewWorkParser().(manifests.Kinded)
	_ manifests.Kinded                                                         = cargo.NewLockParser().(manifests.Kinded)
	_ manifests.Kinded                                                         = npm.NewYarnLock().(manifests.Kinded)
	_ manifests.Kinded                                                         = python.NewUVLock().(manifests.Kinded)
	_ manifests.Kinded                                                         = gradle.NewLockfile().(manifests.Kinded)
	_ manifests.Kinded                                                         = maven.NewConfig().(manifests.Kinded)
)

// Every assembler declares its version (it is part of the registry's SetHash, so a changed assembler rebuilds the cached facts).
var (
	_ manifests.VersionedAssembler = golang.NewAssembler().(manifests.VersionedAssembler)
	_ manifests.VersionedAssembler = npm.NewAssembler().(manifests.VersionedAssembler)
	_ manifests.VersionedAssembler = python.NewAssembler().(manifests.VersionedAssembler)
	_ manifests.VersionedAssembler = maven.NewAssembler().(manifests.VersionedAssembler)
	_ manifests.VersionedAssembler = gradle.NewAssembler().(manifests.VersionedAssembler)
	_ manifests.VersionedAssembler = cargo.NewAssembler().(manifests.VersionedAssembler)
)
